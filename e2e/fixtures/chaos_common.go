/*
 * chaos_common.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package fixtures

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/onsi/gomega"

	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChaosMeshExperiment is a wrapper around an actual chaos mesh experiment and should provide some useful abstractions, to make it easier to run experiments.
type ChaosMeshExperiment struct {
	name        string
	namespace   string
	chaosObject client.Object
}

// ChaosDurationForever represents a very long duration if an experiment should run for the whole test duration.
const ChaosDurationForever = "998h"

// CleanupChaosMeshExperiments deletes any chaos experiments created by this handle.  Invoked at shutdown.  Tests
// that need to delete experiments should invoke Delete on their ChaosMeshExperiment objects.
func (factory *Factory) CleanupChaosMeshExperiments() error {
	if len(factory.chaosExperiments) == 0 {
		return nil
	}

	log.Println(
		"start cleaning up chaos mesh client with",
		len(factory.chaosExperiments),
		"experiment(s)",
	)

	g := new(errgroup.Group)

	for _, resource := range factory.chaosExperiments {
		targetResource := resource // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			err := factory.deleteChaosMeshExperiment(&targetResource)
			if err != nil {
				log.Printf(
					"error in cleaning up chaos experiement %s/%s: %s",
					targetResource.namespace,
					targetResource.name,
					err.Error(),
				)
			}
			return err
		})
	}

	err := g.Wait()
	// Reset the slice
	factory.chaosExperiments = []ChaosMeshExperiment{}

	return err
}

// DeleteChaosMeshExperimentSafe will delete a running Chaos Mesh experiment.
func (factory *Factory) DeleteChaosMeshExperimentSafe(experiment *ChaosMeshExperiment) {
	gomega.Expect(factory.deleteChaosMeshExperiment(experiment)).ToNot(gomega.HaveOccurred())
}

func (factory *Factory) deleteChaosMeshExperiment(experiment *ChaosMeshExperiment) error {
	if experiment == nil {
		return nil
	}

	log.Println("Start deleting", experiment.name)
	err := factory.getChaosExperiment(experiment.name, experiment.namespace, experiment.chaosObject)
	if err != nil {
		// The experiment is already deleted.
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	// Pause the experiment before deleting it: https://chaos-mesh.org/docs/run-a-chaos-experiment/#pause-or-resume-chaos-experiments-using-commands
	// We try this on a best-effort base if this results in an error after 60 seconds we move on and delete the experiment
	annotations := experiment.chaosObject.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[chaosmesh.PauseAnnotationKey] = strconv.FormatBool(
		true,
	) // verbose compared to "true", but fixes annoying linter warning
	experiment.chaosObject.SetAnnotations(annotations)

	err = factory.GetControllerRuntimeClient().Update(context.Background(), experiment.chaosObject)
	if err != nil {
		log.Println("Could not update the annotation to set the experiment into pause state", err)
	}

	err = factory.GetControllerRuntimeClient().Delete(context.Background(), experiment.chaosObject)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	log.Println("Chaos", experiment.name, "is deleted.")
	err = wait.PollUntilContextTimeout(
		context.Background(),
		1*time.Second,
		5*time.Minute,
		true,
		func(_ context.Context) (done bool, err error) {
			err = factory.getChaosExperiment(
				experiment.name,
				experiment.namespace,
				experiment.chaosObject,
			)
			if err != nil && k8serrors.IsNotFound(err) {
				return true, nil
			}

			return false, nil
		},
	)

	if err != nil {
		log.Println("error occurred during experiment deletion", experiment.name)
	}

	return err
}

// getChaosExperiment gets the chaos experiments in the cluster with specified name.
func (factory *Factory) getChaosExperiment(
	name string,
	namespace string,
	chaosOut client.Object,
) error {
	return factory.GetControllerRuntimeClient().Get(context.Background(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, chaosOut)
}

// CreateExperiment creates a chaos experiment in the cluster with specified type, name and chaos object.
func (factory *Factory) CreateExperiment(chaos client.Object) *ChaosMeshExperiment {
	log.Printf("CreateExperiment name=%s, spec=%s", chaos.GetName(), ToJSON(chaos))
	gomega.Expect(factory.CreateIfAbsent(chaos)).NotTo(gomega.HaveOccurred())

	experiment := ChaosMeshExperiment{
		chaosObject: chaos,
		name:        chaos.GetName(),
		namespace:   chaos.GetNamespace(),
	}
	factory.addChaosExperiment(experiment)

	gomega.Expect(factory.waitUntilExperimentRunning(experiment, chaos)).
		NotTo(gomega.HaveOccurred())

	return &experiment
}

func (factory *Factory) waitUntilExperimentRunning(
	experiment ChaosMeshExperiment,
	out client.Object,
) error {
	err := wait.PollUntilContextTimeout(
		context.Background(),
		1*time.Second,
		20*time.Minute,
		true,
		func(_ context.Context) (bool, error) {
			err := factory.getChaosExperiment(experiment.name, experiment.namespace, out)
			if err != nil {
				log.Println("error fetching chaos experiment", err)
				return false, nil
			}

			return isRunning(out)
		},
	)
	if err != nil {
		experiment.chaosObject = out
		return fmt.Errorf("timeout waiting for experiment to be running: %w", err)
	}

	return nil
}

// PodSelector returns the PodSelectorSpec for the provided Pod.
// TODO(j-scheuermann): This should be merged with the method below (PodsSelector).
func PodSelector(pod *corev1.Pod) chaosmesh.PodSelectorSpec {
	pods := make(map[string][]string)
	pods[pod.Namespace] = []string{pod.Name}
	return chaosmesh.PodSelectorSpec{
		Pods: pods,
	}
}

// PodsSelector returns the PodSelectorSpec for the provided Pods.
func PodsSelector(v1pods []corev1.Pod) chaosmesh.PodSelectorSpec {
	pods := make(map[string][]string, len(v1pods))
	for _, pod := range v1pods {
		pods[pod.Namespace] = append(pods[pod.Namespace], pod.Name)
	}
	return chaosmesh.PodSelectorSpec{
		Pods: pods,
	}
}

func chaosNamespaceLabelSelector(
	namespaces []string,
	labelSelector map[string]string,
) chaosmesh.PodSelectorSpec {
	return chaosmesh.PodSelectorSpec{
		GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
			Namespaces:     namespaces,
			LabelSelectors: labelSelector,
		},
	}
}

func chaosNamespaceLabelRequirement(
	namespaces []string,
	labelSelectorRequirement chaosmesh.LabelSelectorRequirements,
) chaosmesh.PodSelectorSpec {
	return chaosmesh.PodSelectorSpec{
		GenericSelectorSpec: chaosmesh.GenericSelectorSpec{
			Namespaces:          namespaces,
			ExpressionSelectors: labelSelectorRequirement,
		},
	}
}

func conditionsAreTrue(status *chaosmesh.ChaosStatus, conditions []chaosmesh.ChaosCondition) bool {
	var allInjected, allSelected bool

	if status == nil {
		log.Println("experiment is missing status information")
		return false
	}

	for _, condition := range conditions {
		if condition.Type == chaosmesh.ConditionAllInjected {
			allInjected = condition.Status == corev1.ConditionTrue
		}

		if condition.Type == chaosmesh.ConditionSelected {
			allSelected = condition.Status == corev1.ConditionTrue
		}
	}

	log.Println(
		"experiment conditions - allInjected:",
		allInjected,
		"allSelected:",
		allSelected,
		"status",
		status,
		"count records",
		len(status.Experiment.Records),
	)

	for _, stat := range status.Experiment.Records {
		log.Println("Records stat ID", stat.Id, "phase:", stat.Phase, "selector", stat.SelectorKey)
	}

	return allInjected && allSelected
}

func isRunning(obj runtime.Object) (bool, error) {
	net, ok := obj.(*chaosmesh.NetworkChaos)
	if ok {
		return conditionsAreTrue(net.GetStatus(), net.GetStatus().Conditions), nil
	}
	io, ok := obj.(*chaosmesh.IOChaos)
	if ok {
		return conditionsAreTrue(io.GetStatus(), io.GetStatus().Conditions), nil
	}
	stress, ok := obj.(*chaosmesh.StressChaos)
	if ok {
		return conditionsAreTrue(stress.GetStatus(), stress.GetStatus().Conditions), nil
	}
	podChaos, ok := obj.(*chaosmesh.PodChaos)
	if ok {
		return conditionsAreTrue(podChaos.GetStatus(), podChaos.GetStatus().Conditions), nil
	}
	httpChaos, ok := obj.(*chaosmesh.HTTPChaos)
	if ok {
		return conditionsAreTrue(httpChaos.GetStatus(), httpChaos.GetStatus().Conditions), nil
	}

	_, ok = obj.(*chaosmesh.Schedule)
	if ok {
		// We could also wait for the first schedule but depending on the provided cron we might wait a long time
		// return !schedule.Status.LastScheduleTime.IsZero(), nil
		return true, nil
	}

	return false, fmt.Errorf(
		"unknown experiment type: %#v",
		obj.GetObjectKind().GroupVersionKind().Kind,
	)
}

// GetOperatorSelector returns the operator Pod selector for chaos mesh.
func GetOperatorSelector(namespace string) chaosmesh.PodSelectorSpec {
	return chaosNamespaceLabelSelector(
		[]string{namespace},
		map[string]string{"app": operatorDeploymentName},
	)
}
