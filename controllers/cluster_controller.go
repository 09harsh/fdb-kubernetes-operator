/*
 * cluster_controller.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2021 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	sigyaml "sigs.k8s.io/yaml"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podclient"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// addPodsReconciler is the reconciler for addPods.
var addPodsReconciler = addPods{}

// subReconcilers has the ordered list of all reconcilers that should be used by the cluster controller.
var subReconcilers = []clusterSubReconciler{
	updateStatus{},
	updateLockConfiguration{},
	updateConfigMap{},
	checkClientCompatibility{},
	deletePodsForBuggification{},
	replaceMisconfiguredProcessGroups{},
	replaceFailedProcessGroups{},
	addProcessGroups{},
	addServices{},
	addPVCs{},
	addPodsReconciler,
	generateInitialClusterFile{},
	removeIncompatibleProcesses{},
	updateSidecarVersions{},
	updatePodConfig{},
	updateMetadata{},
	updateDatabaseConfiguration{},
	chooseRemovals{},
	excludeProcesses{},
	changeCoordinators{},
	bounceProcesses{},
	maintenanceModeChecker{},
	updatePods{},
	removeProcessGroups{},
	removeServices{},
	updateStatus{},
}

// SimulationOptions are used to define how the cluster reconciler should behave in the unit tests.
type SimulationOptions struct {
	// SimulateZones if enabled the zone of pods will be replaced with a "simulation" zone.
	SimulateZones bool
	// SimulateTime is enabled the time checks in the exclude and update pods reconciler will be skipped.
	SimulateTime bool
}

// FoundationDBClusterReconciler reconciles a FoundationDBCluster object
type FoundationDBClusterReconciler struct {
	client.Client
	Recorder                                    record.EventRecorder
	Log                                         logr.Logger
	EnableRestartIncompatibleProcesses          bool
	ServerSideApply                             bool
	EnableRecoveryState                         bool
	CacheDatabaseStatusForReconciliationDefault bool
	ReplaceOnSecurityContextChange              bool
	PodLifecycleManager                         podmanager.PodLifecycleManager
	PodClientProvider                           func(*fdbv1beta2.FoundationDBCluster, *corev1.Pod) (podclient.FdbPodClient, error)
	DatabaseClientProvider                      fdbadminclient.DatabaseClientProvider
	DeprecationOptions                          internal.DeprecationOptions
	GetTimeout                                  time.Duration
	PostTimeout                                 time.Duration
	MinimumRequiredUptimeCCBounce               time.Duration
	MaintenanceListStaleDuration                time.Duration
	MaintenanceListWaitDuration                 time.Duration
	// GlobalSynchronizationWaitDuration is the wait time for the operator when the synchronization mode is set to
	// global. The wait time defines the period where no updates for the according action should happen. Increasing the
	// wait time will increase the chances that all updates are part of the list but will also delay the rollout of
	// the change.
	GlobalSynchronizationWaitDuration time.Duration
	// MinimumRecoveryTimeForInclusion defines the duration in seconds that a cluster must be up
	// before new inclusions are allowed. The operator issuing frequent inclusions in a short time window
	// could cause instability for the cluster as each inclusion will/can cause a recovery. Delaying the inclusion
	// of deleted process groups is not an issue as all the process groups that have no resources and are marked for
	// deletion and are fully excluded, will be batched together in a single inclusion call.
	MinimumRecoveryTimeForInclusion float64
	// MinimumRecoveryTimeForExclusion defines the duration in seconds that a cluster must be up
	// before new exclusions are allowed. The operator issuing frequent exclusions in a short time window
	// could cause instability for the cluster as each exclusion will/can cause a recovery.
	MinimumRecoveryTimeForExclusion float64
	// Namespace for the FoundationDBClusterReconciler, if empty the FoundationDBClusterReconciler will watch all namespaces.
	Namespace string
	// ClusterLabelKeyForNodeTrigger if set will trigger a reconciliation for all FoundationDBClusters that host a Pod
	// on the affected node.
	ClusterLabelKeyForNodeTrigger string
	decodingSerializer            runtime.Serializer
	SimulationOptions             SimulationOptions
}

// NewFoundationDBClusterReconciler creates a new FoundationDBClusterReconciler with defaults.
func NewFoundationDBClusterReconciler(
	podLifecycleManager podmanager.PodLifecycleManager,
) *FoundationDBClusterReconciler {
	r := &FoundationDBClusterReconciler{
		PodLifecycleManager: podLifecycleManager,
	}
	r.PodClientProvider = r.newFdbPodClient
	r.decodingSerializer = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	return r
}

// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods;configmaps;persistentvolumeclaims;events;secrets;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile runs the reconciliation logic.
func (r *FoundationDBClusterReconciler) Reconcile(
	ctx context.Context,
	request ctrl.Request,
) (ctrl.Result, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}

	err := r.Get(ctx, request.NamespacedName, cluster)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	clusterLog := globalControllerLogger.WithValues(
		"namespace",
		cluster.Namespace,
		"cluster",
		cluster.Name,
		"traceID",
		uuid.NewUUID(),
	)
	cacheStatus := cluster.CacheDatabaseStatusForReconciliation(
		r.CacheDatabaseStatusForReconciliationDefault,
	)
	// Printout the duration of the reconciliation, independent if the reconciliation was successful or had an error.
	startTime := time.Now()
	defer func() {
		clusterLog.Info(
			"Reconciliation run finished",
			"duration_seconds",
			time.Since(startTime).Seconds(),
			"cacheStatus",
			cacheStatus,
		)
	}()

	if cluster.Spec.Skip {
		clusterLog.Info("Skipping cluster with skip value true", "skip", cluster.Spec.Skip)
		// Don't requeue
		return ctrl.Result{}, nil
	}

	err = internal.NormalizeClusterSpec(cluster, r.DeprecationOptions)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = cluster.Validate()
	if err != nil {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, "ClusterSpec not valid", err.Error())
		return ctrl.Result{}, fmt.Errorf("ClusterSpec is not valid: %w", err)
	}

	adminClient, err := r.getAdminClient(clusterLog, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		_ = adminClient.Close()
	}()
	supportedVersion, err := adminClient.VersionSupported(cluster.Spec.Version)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !supportedVersion {
		return ctrl.Result{}, fmt.Errorf("version %s is not supported", cluster.Spec.Version)
	}

	// When using DNS entries in the cluster file, we want to make sure to create pods if required before doing any
	// interaction with the FDB cluster. If none of the coordinator pods is running, this will cause a crash in the FDB
	// go bindings (or rather the C client). To prevent those case we run the addPods reconciler before interacting with
	// the FoundationDB cluster.
	if cluster.UseDNSInClusterFile() {
		req := runClusterSubReconciler(ctx, clusterLog, addPodsReconciler, r, cluster, nil)
		if req != nil && !req.delayedRequeue {
			return processRequeue(req, addPodsReconciler, cluster, r.Recorder, clusterLog)
		}
	}

	var status *fdbv1beta2.FoundationDBStatus
	if cacheStatus {
		clusterLog.Info(
			"Fetch machine-readable status for reconciliation loop",
			"cacheStatus",
			cacheStatus,
		)
		status, err = r.getStatusFromClusterOrDummyStatus(clusterLog, cluster)
		if err != nil {
			clusterLog.Info(
				"could not fetch machine-readable status and therefore didn't cache it",
				"error",
				err.Error(),
			)
			// In our e2e test cases we have observed cases where the operator got stuck fetching the latest machine-readable status from a cluster
			// when all coordinator pods got restarted. This only happens when DNS entries are used in the connection string (otherwise the operator
			// will be stuck). During testing we observed that restarting the operator process resolved the issue.
			//
			// See: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/2311
			if cluster.UseDNSInClusterFile() &&
				strings.Contains(err.Error(), "FoundationDB error code 1512") {
				clusterLog.V(0).Info("try to resolve addresses")
				parsedConnectionString, err := fdbv1beta2.ParseConnectionString(
					cluster.Status.ConnectionString,
				)
				if err != nil {
					clusterLog.V(0).
						Info("could not try to resolve coordinator addresses", "error", err.Error())
				} else {
					coordinatorsResolvable := true
					resolver := net.Resolver{}
					// Try to resolve the coordinators. If the operator is able to resolve the coordinators but the FDB bindings are not able
					// to do it, we have to restart the operator process.
					for _, coordinator := range parsedConnectionString.Coordinators {
						addr, err := fdbv1beta2.ParseProcessAddress(coordinator)
						if err != nil {
							continue
						}

						// If an IP address is set we ignore this entry. In theory, it's possible that the connection string
						// contains a mix of DNS entries and IP addresses. For clusters that are manged by the operator, this
						// shouldn't be the case.
						if !addr.IsEmpty() {
							continue
						}

						// Try to resolve all the coordinators.
						name := addr.MachineAddress()
						clusterLog.V(0).Info("trying to resolve coordinator", "coordinator", name)

						ip, err := resolver.LookupIPAddr(ctx, name)
						clusterLog.V(0).Info("resolved coordinator", "coordinator", name, "ip", ip, "error", err)
						if err != nil || ip == nil {
							coordinatorsResolvable = false
						}
					}

					// If all coordinators are resolvable shutdown the operator process by sending a SIGTERM to ensure
					// a graceful shutdown can be initiated.
					if coordinatorsResolvable {
						clusterLog.Info("trying to shutdown process because all coordinators are resolvable but the go bindings return a binding error")
						process, err := os.FindProcess(os.Getpid())
						if err != nil {
							fmt.Printf("Error finding process: %v\n", err)
							return ctrl.Result{RequeueAfter: 5 * time.Second}, err
						}

						err = process.Signal(syscall.SIGTERM)
						if err != nil {
							fmt.Printf("Error sending signal: %v\n", err)
							return ctrl.Result{RequeueAfter: 5 * time.Second}, err
						}
					}
				}
			}
		}
	}

	originalGeneration := cluster.ObjectMeta.Generation
	normalizedSpec := cluster.Spec.DeepCopy()
	var delayedRequeueDuration time.Duration
	var delayedRequeue bool

	for _, subReconciler := range subReconcilers {
		// We have to set the normalized spec here again otherwise any call to Update() for the status of the cluster
		// will reset all normalized fields...
		cluster.Spec = *(normalizedSpec.DeepCopy())

		req := runClusterSubReconciler(ctx, clusterLog, subReconciler, r, cluster, status)
		if req == nil {
			continue
		}

		if req.delayedRequeue {
			clusterLog.Info("Delaying requeue for sub-reconciler",
				"reconciler", fmt.Sprintf("%T", subReconciler),
				"message", req.message,
				"delayedRequeueDuration", delayedRequeueDuration.String(),
				"error", req.curError)
			if delayedRequeueDuration < req.delay {
				delayedRequeueDuration = req.delay
			}

			delayedRequeue = true
			continue
		}

		return processRequeue(req, subReconciler, cluster, r.Recorder, clusterLog)
	}

	if cluster.Status.Generations.Reconciled < originalGeneration || delayedRequeue {
		clusterLog.Info(
			"Cluster was not fully reconciled by reconciliation process",
			"status",
			cluster.Status.Generations,
			"CurrentGeneration",
			cluster.Status.Generations.Reconciled,
			"OriginalGeneration",
			originalGeneration,
			"DelayedRequeue",
			delayedRequeueDuration.String(),
		)

		if delayedRequeueDuration == time.Duration(0) {
			delayedRequeueDuration = 2 * time.Second
		}

		return ctrl.Result{RequeueAfter: delayedRequeueDuration}, nil
	}

	clusterLog.Info("Reconciliation complete", "generation", cluster.Status.Generations.Reconciled)
	r.Recorder.Event(
		cluster,
		corev1.EventTypeNormal,
		"ReconciliationComplete",
		fmt.Sprintf("Reconciled generation %d", cluster.Status.Generations.Reconciled),
	)

	return ctrl.Result{}, nil
}

// runClusterSubReconciler will start the subReconciler and will log the duration of the subReconciler.
func runClusterSubReconciler(
	ctx context.Context,
	logger logr.Logger,
	subReconciler clusterSubReconciler,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
) *requeue {
	subReconcileLogger := logger.WithValues("reconciler", fmt.Sprintf("%T", subReconciler))
	startTime := time.Now()
	subReconcileLogger.Info("Attempting to run sub-reconciler")
	defer func() {
		subReconcileLogger.Info(
			"Subreconciler finished run",
			"duration_seconds",
			time.Since(startTime).Seconds(),
		)
	}()

	return subReconciler.reconcile(ctx, r, cluster, status, subReconcileLogger)
}

// updateIndexerForManager will set all the required field indexer for the FoundationDBClusterReconciler.
func (r *FoundationDBClusterReconciler) updateIndexerForManager(mgr ctrl.Manager) error {
	if r.ClusterLabelKeyForNodeTrigger == "" {
		return nil
	}

	return mgr.GetFieldIndexer().
		IndexField(context.Background(), &corev1.Pod{}, "spec.nodeName", func(o client.Object) []string {
			return []string{o.(*corev1.Pod).Spec.NodeName}
		})
}

// SetupWithManager prepares the FoundationDBClusterReconciler for use.
func (r *FoundationDBClusterReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrentReconciles int,
	selector metav1.LabelSelector,
	watchedObjects ...client.Object,
) error {
	err := r.updateIndexerForManager(mgr)
	if err != nil {
		return err
	}

	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(selector)
	if err != nil {
		return err
	}

	// Only react on generation changes or annotation changes and only watch
	// resources with the provided label selector.
	// We cannot use the WithEventFilter method as that would also add the predicate to the node watch.
	// See: https://github.com/kubernetes-sigs/controller-runtime/issues/2785
	globalPredicate := builder.WithPredicates(predicate.And(
		labelSelectorPredicate,
		predicate.Or(
			predicate.TypedLabelChangedPredicate[client.Object]{},
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		),
	))

	managerBuilder := ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles},
		).
		For(&fdbv1beta2.FoundationDBCluster{}, globalPredicate).
		Owns(&corev1.Pod{}, globalPredicate).
		Owns(&corev1.PersistentVolumeClaim{}, globalPredicate).
		Owns(&corev1.ConfigMap{}, globalPredicate).
		Owns(&corev1.Service{}, globalPredicate)

	if r.ClusterLabelKeyForNodeTrigger != "" {
		managerBuilder.Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.findFoundationDBClusterForNode),
			builder.WithPredicates(
				internal.NodeTaintChangedPredicate{
					Logger: r.Log.WithName("NodeTaintChangedPredicate"),
				},
			),
		)
	}

	for _, object := range watchedObjects {
		managerBuilder.Owns(object)
	}

	return managerBuilder.Complete(r)
}

// // MapFunc is the signature required for enqueueing requests from a generic function.
//// This type is usually used with EnqueueRequestsFromMapFunc when registering an event handler.
//type MapFunc = TypedMapFunc[client.Object, reconcile.Request]

// findFoundationDBClusterForNode will filter out all associated FoundationDBClusters that have a Pod running on that
// specific node.
func (r *FoundationDBClusterReconciler) findFoundationDBClusterForNode(
	ctx context.Context,
	node client.Object,
) []reconcile.Request {
	logger := r.Log.WithValues("node", node.GetName())
	podsOnNode := &corev1.PodList{}

	labelSelector := client.HasLabels([]string{r.ClusterLabelKeyForNodeTrigger})

	listOpts := []client.ListOption{
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("spec.nodeName", node.GetName()),
		},
		labelSelector,
	}

	if r.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(r.Namespace))
	}

	err := r.List(ctx, podsOnNode, listOpts...)
	if err != nil {
		logger.Error(err, "Processing findFoundationDBClusterForNode could not fetch Pods on node")
		return []reconcile.Request{}
	}

	if len(podsOnNode.Items) == 0 {
		return []reconcile.Request{}
	}

	logger.V(1).
		Info("Processing findFoundationDBClusterForNode, found Pods on node that changed", "labelSelector", r.ClusterLabelKeyForNodeTrigger, "podsOnNode", len(podsOnNode.Items))

	requests := make([]reconcile.Request, len(podsOnNode.Items))
	for i, item := range podsOnNode.Items {
		// Since we use a label selector all Pods should have the cluster label.
		clusterName, ok := item.GetLabels()[r.ClusterLabelKeyForNodeTrigger]
		if !ok {
			logger.V(1).Info("Missing cluster label information", "triggeringPod", item.Name)
			continue
		}

		logger.V(1).
			Info("Processing findFoundationDBClusterForNode, found cluster that needs an update", "triggeringPod", item.Name, "clusterName", clusterName)
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      clusterName,
				Namespace: item.GetNamespace(),
			},
		}
	}

	return requests
}

func (r *FoundationDBClusterReconciler) updatePodDynamicConf(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod,
) (bool, error) {
	if cluster.ProcessGroupIsBeingRemoved(podmanager.GetProcessGroupID(cluster, pod)) {
		return true, nil
	}

	podClient, message := r.getPodClient(cluster, pod)
	if podClient == nil {
		logger.Info("Unable to generate pod client", "message", message)
		return false, nil
	}

	processClass, err := podmanager.GetProcessClass(cluster, pod)
	if err != nil {
		return false, err
	}

	serversPerPod, err := internal.GetServersPerPodForPod(pod, processClass)
	if err != nil {
		return false, err
	}

	var expectedConf string

	imageType := internal.GetImageType(pod)
	if imageType == fdbv1beta2.ImageTypeUnified {
		config := internal.GetMonitorProcessConfiguration(
			cluster,
			processClass,
			serversPerPod,
			imageType,
		)
		configData, err := json.Marshal(config)
		if err != nil {
			return false, err
		}
		expectedConf = string(configData)
	} else {
		expectedConf, err = internal.GetMonitorConf(cluster, processClass, podClient, serversPerPod)
		if err != nil {
			return false, err
		}
	}

	syncedFDBCluster, clusterErr := podClient.UpdateFile(
		"fdb.cluster",
		cluster.Status.ConnectionString,
	)
	syncedFDBMonitor, err := podClient.UpdateFile("fdbmonitor.conf", expectedConf)
	if !syncedFDBCluster || !syncedFDBMonitor {
		if clusterErr != nil {
			return false, clusterErr
		}

		return false, err
	}

	if cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
		return podClient.IsPresent(fmt.Sprintf("bin/%s/fdbserver", cluster.Spec.Version))
	}

	return true, nil
}

func (r *FoundationDBClusterReconciler) getPodClient(
	cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod,
) (podclient.FdbPodClient, string) {
	if pod == nil {
		return nil, fmt.Sprintf(
			"Process group in cluster %s/%s does not have pod defined",
			cluster.Namespace,
			cluster.Name,
		)
	}

	podClient, err := r.PodClientProvider(cluster, pod)
	if err != nil {
		return nil, err.Error()
	}

	return podClient, ""
}

// getDatabaseClientProvider gets the client provider for a reconciler.
func (r *FoundationDBClusterReconciler) getDatabaseClientProvider() fdbadminclient.DatabaseClientProvider {
	if r.DatabaseClientProvider != nil {
		return r.DatabaseClientProvider
	}

	panic("Cluster reconciler does not have a DatabaseClientProvider defined")
}

// getAdminClient gets the admin client for a reconciler.
func (r *FoundationDBClusterReconciler) getAdminClient(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
) (fdbadminclient.AdminClient, error) {
	return r.getDatabaseClientProvider().GetAdminClientWithLogger(cluster, r, logger)
}

func (r *FoundationDBClusterReconciler) getLockClient(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
) (fdbadminclient.LockClient, error) {
	return r.getDatabaseClientProvider().GetLockClientWithLogger(cluster, logger)
}

// takeLock attempts to acquire a lock.
func (r *FoundationDBClusterReconciler) takeLock(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	action string,
) error {
	if !cluster.ShouldUseLocks() {
		return nil
	}

	logger.Info("Taking lock on cluster", "action", action)
	lockClient, err := r.getLockClient(logger, cluster)
	if err != nil {
		return err
	}

	return lockClient.TakeLock()
}

// releaseLock attempts to release a lock.
func (r *FoundationDBClusterReconciler) releaseLock(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
) error {
	lockClient, err := r.getLockClient(logger, cluster)
	if err != nil {
		return err
	}

	return lockClient.ReleaseLock()
}

// clusterSubReconciler describes a class that does part of the work of
// reconciliation for a cluster.
type clusterSubReconciler interface {
	/**
	reconcile runs the reconciler's work.

	If reconciliation can continue, this should return nil.

	If reconciliation encounters an error, this should return a	requeue object
	with an `Error` field.

	If reconciliation cannot proceed, this should return a requeue object with
	a `Message` field.
	*/
	reconcile(
		ctx context.Context,
		r *FoundationDBClusterReconciler,
		cluster *fdbv1beta2.FoundationDBCluster,
		status *fdbv1beta2.FoundationDBStatus,
		logger logr.Logger,
	) *requeue
}

// newFdbPodClient builds a client for working with an FDB Pod
func (r *FoundationDBClusterReconciler) newFdbPodClient(
	cluster *fdbv1beta2.FoundationDBCluster,
	pod *corev1.Pod,
) (podclient.FdbPodClient, error) {
	return internal.NewFdbPodClient(
		cluster,
		pod,
		globalControllerLogger.WithValues(
			"namespace",
			cluster.Namespace,
			"cluster",
			cluster.Name,
			"pod",
			pod.Name,
		),
		r.GetTimeout,
		r.PostTimeout,
	)
}

// updateOrApply updates the status either with server-side apply or if disabled with the normal update call.
func (r *FoundationDBClusterReconciler) updateOrApply(
	ctx context.Context,
	cluster *fdbv1beta2.FoundationDBCluster,
) error {
	if r.ServerSideApply {
		// We have to set the TypeMeta otherwise the Patch command will fail. This is the rudimentary
		// support for server side apply which should be enough for the status use case. The controller runtime will
		// add some additional support in the future: https://github.com/kubernetes-sigs/controller-runtime/issues/347.
		patch := &fdbv1beta2.FoundationDBCluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       cluster.Kind,
				APIVersion: cluster.APIVersion,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
			Status: cluster.Status,
		}

		// We are converting the patch into an *unstructured.Unstructured to remove fields that use a default value.
		// If we are not doing this, empty (nil) fields will be evaluated as if they were set by the default value.
		// In some previous testing we discovered some issues with that behaviour. With the *unstructured.Unstructured
		// we make sure that only fields that are actually set will be applied.
		outBytes, err := sigyaml.Marshal(patch)
		if err != nil {
			return err
		}

		unstructuredPatch := &unstructured.Unstructured{}
		_, _, err = r.decodingSerializer.Decode(outBytes, nil, unstructuredPatch)
		if err != nil {
			return err
		}

		return r.Status().
			Patch(ctx, unstructuredPatch, client.Apply, client.FieldOwner("fdb-operator"), client.ForceOwnership)
	}

	return r.Status().Update(ctx, cluster)
}

// getStatusFromClusterOrDummyStatus will fetch the machine-readable status from the FoundationDBCluster if the cluster is configured. If not a default status is returned indicating, that
// some configuration is missing.
func (r *FoundationDBClusterReconciler) getStatusFromClusterOrDummyStatus(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
) (*fdbv1beta2.FoundationDBStatus, error) {
	if cluster.Status.ConnectionString == "" {
		return &fdbv1beta2.FoundationDBStatus{
			Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
				Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
					Error: "configurationMissing",
				},
			},
		}, nil
	}

	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = adminClient.Close()
	}()

	// If the cluster is not yet configured, we can reduce the timeout to make sure the initial reconcile steps
	// are faster.
	if !cluster.Status.Configured {
		adminClient.SetTimeout(10 * time.Second)
	}

	status, err := adminClient.GetStatus()
	if err == nil {
		// Update the connection string if the newly fetched connection string is different from the current one and if the
		// newly fetched connection string is not empty.
		if cluster.Status.ConnectionString != status.Cluster.ConnectionString &&
			status.Cluster.ConnectionString != "" {
			logger.Info(
				"Updating out-of-date connection string",
				"previousConnectionString",
				cluster.Status.ConnectionString,
				"newConnectionString",
				status.Cluster.ConnectionString,
			)
			r.Recorder.Event(
				cluster,
				corev1.EventTypeNormal,
				"UpdatingConnectionString",
				fmt.Sprintf("Setting connection string to %s", status.Cluster.ConnectionString),
			)
			cluster.Status.ConnectionString = status.Cluster.ConnectionString
		}

		return status, nil
	}

	// When we reached this part of the code the above GetStatus() called failed for some reason.
	if cluster.Status.Configured {
		// If the cluster is currently under a version incompatible upgrade, we try to assume the current version based
		// on the coordinator reachability. If all (or the majority) of coordinators are reachable with a specific version
		// of fdbcli we can assume that the cluster is running with that version and update the cluster.Status.RunningVersion.
		// In theory we could use the go bindings if they would expose that information from the multi-version bindings.
		if cluster.IsBeingUpgradedWithVersionIncompatibleVersion() {
			// Set the version from the reachable coordinators, if the version points to the desired version defined
			// in the cluster.Spec.Version, this will unblock some further steps, to allow the operator to bring the cluster
			// back into a better state.
			versionFromReachableCoordinators := adminClient.GetVersionFromReachableCoordinators()
			if versionFromReachableCoordinators != "" &&
				versionFromReachableCoordinators != cluster.Status.RunningVersion {
				logger.Info(
					"Update running version in cluster status from reachable coordinators",
					"versionFromReachableCoordinators",
					versionFromReachableCoordinators,
					"currentRunningVersion",
					cluster.Status.RunningVersion,
				)
				cluster.Status.RunningVersion = versionFromReachableCoordinators
			}
		}

		return nil, err
	}

	return &fdbv1beta2.FoundationDBStatus{
		Cluster: fdbv1beta2.FoundationDBStatusClusterInfo{
			Layers: fdbv1beta2.FoundationDBStatusLayerInfo{
				Error: "configurationMissing",
			},
		},
	}, nil
}
