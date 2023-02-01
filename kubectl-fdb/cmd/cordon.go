/*
 * cordon.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package cmd

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"
)

func newCordonCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)
	var nodeSelectors map[string]string

	cmd := &cobra.Command{
		Use:   "cordon",
		Short: "Adds all process groups (or multiple) that run on a node to the remove list of the given cluster",
		Long:  "Adds all process groups (or multiple) that run on a node to the remove list of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			sleep, err := cmd.Root().Flags().GetUint16("sleep")
			if err != nil {
				return err
			}
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			nodeSelector, err := cmd.Flags().GetStringToString("node-selector")
			if err != nil {
				return err
			}
			customLabels, err := cmd.Flags().GetStringArray("custom-labels")
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(o)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			if len(nodeSelector) != 0 && len(args) != 0 {
				return fmt.Errorf("it's not allowed to use the node-selector and pass nodes")
			}

			if len(nodeSelector) != 0 {
				nodes, err := getNodes(kubeClient, nodeSelector)
				if err != nil {
					return err
				}

				return cordonNode(kubeClient, clusterName, nodes, namespace, withExclusion, wait, sleep, customLabels)
			}

			return cordonNode(kubeClient, clusterName, args, namespace, withExclusion, wait, sleep, customLabels)
		},
		Example: `
# Evacuate all process groups for a cluster in the current namespace that are hosted on node-1
kubectl fdb cordon -c cluster node-1

# Evacuate all process groups for a cluster in the default namespace that are hosted on node-1
kubectl fdb cordon -n default -c cluster node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on node-1, the default label is fdb-cluster-name
kubectl fdb cordon node-1

# Evacuate all process groups in the current namespace that are hosted on node-1 with custom label
kubectl fdb cordon -l "fdb-cluster-name fdb-cluster-group" node-1

# Evacuate all process groups for a cluster in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon -c cluster --node-selector machine=a,disk=fast

# Evacuate all process groups in the current namespace that are hosted on nodes with the labels machine=a,disk=fast
kubectl fdb cordon --node-selector machine=a,disk=fast
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "evacuate process group(s) from the provided cluster.")
	cmd.Flags().StringToStringVarP(&nodeSelectors, "node-selector", "", nil, "node-selector to select all nodes that should be cordoned. Can't be used with specific nodes.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the process groups should be removed with exclusion.")
	cmd.Flags().StringArrayP("custom-labels", "l", []string{"fdb-cluster-name"}, "space separated custom label to extract appropriate pods")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func getClusterNames(kubeClient client.Client, inputClusterName string, namespace string, node string, customLabels []string) ([]string, error) {
	if len(inputClusterName) != 0 {
		// Cluster name already given.
		return []string{inputClusterName}, nil
	}
	var pods corev1.PodList
	err := kubeClient.List(ctx.Background(), &pods,
		client.InNamespace(namespace),
		client.HasLabels(customLabels),
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("spec.nodeName", node),
		})
	if err != nil {
		return nil, fmt.Errorf("error fetching pods with custom labels %v", customLabels)
	}
	clusterNames := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		clusterName, ok := pod.Labels["fdb-cluster-name"]
		if !ok {
			fmt.Printf("could not fetch cluster name from Pod: %s\n", pod.Name)
			continue
		}
		clusterNames = append(clusterNames, clusterName)
	}
	return clusterNames, nil
}

// cordonNode gets all process groups of this cluster that run on the given nodes and add them to the remove list
func cordonNode(kubeClient client.Client, inputClusterName string, nodes []string, namespace string, withExclusion bool, wait bool, sleep uint16, customLabels []string) error {
	fmt.Printf("Start to cordon %d nodes\n", len(nodes))
	if len(nodes) == 0 {
		return nil
	}

	operationFailed := false
	for _, node := range nodes {
		clusterNames, err := getClusterNames(kubeClient, inputClusterName, namespace, node, customLabels)
		if err != nil {
			return fmt.Errorf("unable to fetch cluster names")
		}
		for _, clusterName := range clusterNames {
			fmt.Printf("Starting operation on %s\n", clusterName)
			cluster, err := loadCluster(kubeClient, namespace, clusterName)
			if err != nil {
				fmt.Printf("unable to load cluster: %s, skipping\n", clusterName)
				operationFailed = true
				continue
			}
			var pods corev1.PodList
			err = kubeClient.List(ctx.Background(), &pods,
				client.InNamespace(namespace),
				client.MatchingLabels(cluster.GetMatchLabels()),
				client.MatchingFieldsSelector{
					Selector: fields.OneTermEqualSelector("spec.nodeName", node),
				})
			if err != nil {
				return err
			}
			var processGroups []string
			for _, pod := range pods.Items {
				// With the field selector above this shouldn't be required, but it's good to
				// have a second check.
				if pod.Spec.NodeName != node {
					fmt.Printf("Pod: %s is not running on node %s will be ignored\n", pod.Name, node)
					continue
				}

				processGroup, ok := pod.Labels[cluster.GetProcessGroupIDLabel()]
				if !ok {
					fmt.Printf("could not fetch process group ID from Pod: %s\n", pod.Name)
					continue
				}
				processGroups = append(processGroups, processGroup)
			}
			err = replaceProcessGroups(kubeClient, cluster.Name, processGroups, namespace, withExclusion, wait, false, true, sleep)
			if err != nil {
				operationFailed = true
				fmt.Printf("unable to cordon all pods for cluster %s", cluster.Name)
			}
		}
	}
	if operationFailed {
		return fmt.Errorf("one or more operation failed, please rechecka and retry")
	}
	return nil
}
