/*
 * operator_plugin_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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

package operator

/*
This test suite includes functional tests for the kubectl-fdb plugin.
*/

import (
	"context"
	"fmt"
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"
)

var (
	factory       *fixtures.Factory
	fdbCluster    *fixtures.HaFdbCluster
	testOptions   *fixtures.FactoryOptions
	clusterConfig *fixtures.ClusterConfig
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	clusterConfig = fixtures.DefaultClusterConfigWithHaMode(fixtures.HaFourZoneSingleSat, false)
	fdbCluster = factory.CreateFdbHaCluster(clusterConfig)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator Plugin", Label("e2e", "pr"), func() {
	When("getting the plugin version from the operator pod", func() {
		It("should print the version", func() {
			// Pick one operator pod and execute the kubectl version command to ensure that kubectl-fdb is present
			// and can be executed.
			operatorPod := factory.RandomPickOnePod(
				factory.GetOperatorPods(fdbCluster.GetPrimary().Namespace()).Items,
			)
			log.Println("operatorPod:", operatorPod.Name)
			Eventually(func(g Gomega) string {
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					context.Background(),
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s --version-check=false version",
						fdbCluster.GetPrimary().Namespace(),
					),
					false,
				)
				g.Expect(err).NotTo(HaveOccurred(), stderr)
				return stdout
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(And(ContainSubstring("kubectl-fdb build information:"), ContainSubstring("foundationdb-operator:")))
		})
	})

	When("all Pods in the primary and satellites are down", func() {
		var useDNS bool

		JustBeforeEach(func() {
			var errGroup errgroup.Group
			// Enable DNS names in the cluster file for the whole cluster.
			for _, cluster := range fdbCluster.GetAllClusters() {
				target := cluster
				errGroup.Go(func() error {
					return target.SetUseDNSInClusterFile(useDNS)
				})
			}
			Expect(errGroup.Wait()).NotTo(HaveOccurred())

			for _, cluster := range fdbCluster.GetAllClusters() {
				Expect(cluster.GetCluster().UseDNSInClusterFile()).To(Equal(useDNS))
			}

			// This tests is a destructive test where the cluster will stop working for some period.
			primary := fdbCluster.GetPrimary()
			primary.SetSkipReconciliation(true)

			primarySatellite := fdbCluster.GetPrimarySatellite()
			primarySatellite.SetSkipReconciliation(true)

			remoteSatellite := fdbCluster.GetRemoteSatellite()
			remoteSatellite.SetSkipReconciliation(true)

			remote := fdbCluster.GetRemote()
			remote.SetSkipReconciliation(true)

			var wg errgroup.Group
			log.Println("Delete Pods in primary")
			wg.Go(func() error {
				return factory.GetControllerRuntimeClient().
					DeleteAllOf(context.Background(), &corev1.Pod{}, ctrlClient.MatchingLabels(primary.GetResourceLabels()), ctrlClient.InNamespace(primary.Namespace()))
			})

			log.Println("Delete Pods in primary satellite")
			wg.Go(func() error {
				return factory.GetControllerRuntimeClient().
					DeleteAllOf(context.Background(), &corev1.Pod{}, ctrlClient.MatchingLabels(primarySatellite.GetResourceLabels()), ctrlClient.InNamespace(primarySatellite.Namespace()))
			})

			log.Println("Delete Pods in remote satellite")
			wg.Go(func() error {
				return factory.GetControllerRuntimeClient().
					DeleteAllOf(context.Background(), &corev1.Pod{}, ctrlClient.MatchingLabels(remoteSatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))
			})

			Expect(wg.Wait()).NotTo(HaveOccurred())
			// Wait a short amount of time to let the cluster see that the primary and primary satellite is down.
			time.Sleep(5 * time.Second)

			// Ensure that all the pods are deleted.
			Eventually(func(g Gomega) []corev1.Pod {
				pods := &corev1.PodList{}
				g.Expect(factory.GetControllerRuntimeClient().List(context.Background(), pods, ctrlClient.MatchingLabels(primary.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
					To(Succeed())

				return pods.Items
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())

			Eventually(func(g Gomega) []corev1.Pod {
				pods := &corev1.PodList{}
				g.Expect(factory.GetControllerRuntimeClient().List(context.Background(), pods, ctrlClient.MatchingLabels(primarySatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
					To(Succeed())

				return pods.Items
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())

			Eventually(func(g Gomega) []corev1.Pod {
				pods := &corev1.PodList{}
				g.Expect(factory.GetControllerRuntimeClient().List(context.Background(), pods, ctrlClient.MatchingLabels(remoteSatellite.GetResourceLabels()), ctrlClient.InNamespace(remoteSatellite.Namespace()))).
					To(Succeed())

				return pods.Items
			}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeEmpty())
		})

		// Default case is to run with DNS enabled. The test case with IPs enabled can run into issues when
		// the underlying Kubernetes cluster deletes pods.
		// Because of the above issues the test case is currently disabled (marked as pending) and can be used
		// to run the test manually if needed.
		PWhen("DNS is disabled", func() {
			BeforeEach(func() {
				useDNS = false
			})

			It("should recover the coordinators", func() {
				remote := fdbCluster.GetRemote()
				// Pick one operator pod and execute the recovery command
				operatorPod := factory.RandomPickOnePod(
					factory.GetOperatorPods(remote.Namespace()).Items,
				)
				log.Println("operatorPod:", operatorPod.Name)
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					context.Background(),
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s recover-multi-region-cluster --version-check=false --wait=false %s",
						remote.Namespace(),
						remote.Name(),
					),
					false,
				)
				log.Println("stdout:", stdout, "stderr:", stderr)
				Expect(err).NotTo(HaveOccurred())

				// Ensure the cluster is available again.
				Eventually(func() bool {
					return remote.GetStatus().Client.DatabaseStatus.Available
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

				remote.SetSkipReconciliation(false)
				// Recreate the operator pods to ensure they get the new connection string.
				factory.RecreateOperatorPods(remote.Namespace())
				// Ensure that the cluster is able to reconcile
				Expect(remote.WaitForReconciliation()).To(Succeed())

				log.Println("new connection string:", remote.GetCluster().Status.ConnectionString)
				connectionString, err := fdbv1beta2.ParseConnectionString(
					remote.GetCluster().Status.ConnectionString,
				)
				Expect(err).NotTo(HaveOccurred())

				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbv1beta2.ParseProcessAddress(coordinator)
					Expect(err).NotTo(HaveOccurred())
					Expect(address.StringAddress).To(BeEmpty())
				}
			})
		})

		When("DNS names in the cluster file are used", func() {
			BeforeEach(func() {
				useDNS = true
			})

			It("should recover the coordinators", func() {
				remote := fdbCluster.GetRemote()
				// Pick one operator pod and execute the recovery command
				operatorPod := factory.RandomPickOnePod(
					factory.GetOperatorPods(remote.Namespace()).Items,
				)
				log.Println("operatorPod:", operatorPod.Name)
				stdout, stderr, err := factory.ExecuteCmdOnPod(
					context.Background(),
					&operatorPod,
					"manager",
					fmt.Sprintf(
						"kubectl-fdb -n %s recover-multi-region-cluster --version-check=false --wait=false %s",
						remote.Namespace(),
						remote.Name(),
					),
					false,
				)
				log.Println("stdout:", stdout, "stderr:", stderr)
				Expect(err).NotTo(HaveOccurred())

				// Ensure the cluster is available again.
				Eventually(func() bool {
					return remote.GetStatus().Client.DatabaseStatus.Available
				}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).Should(BeTrue())

				currentConnectionString := remote.GetStatus().Cluster.ConnectionString
				log.Println("new connection string:", currentConnectionString)
				connectionString, err := fdbv1beta2.ParseConnectionString(currentConnectionString)
				Expect(err).NotTo(HaveOccurred())

				for _, coordinator := range connectionString.Coordinators {
					address, err := fdbv1beta2.ParseProcessAddress(coordinator)
					log.Println("address", address)
					Expect(err).NotTo(HaveOccurred())
					Expect(address.StringAddress).NotTo(BeEmpty())
				}
			})
		})

		AfterEach(func() {
			log.Println("Recreate cluster")
			// Delete the broken cluster.
			factory.Shutdown()
			// Recreate the cluster to make sure  the next tests can proceed
			factory = fixtures.CreateFactory(testOptions)
			fdbCluster = factory.CreateFdbHaCluster(clusterConfig)
		})
	})
})
