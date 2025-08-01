/*
 * fdb_operator_fixtures.go
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
	"fmt"
	"log"

	"github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

func (factory *Factory) ensureFdbClusterExists(
	clusterSpec *fdbv1beta2.FoundationDBCluster,
	config *ClusterConfig,
) (*FdbCluster, error) {
	clusterStatus, err := factory.getClusterStatus(clusterSpec.Name, clusterSpec.Namespace)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("could not look up FDB cluster: %w", err)
	}
	if err == nil {
		log.Printf("reuse cluster: %s/%s", clusterSpec.Namespace, clusterStatus.Name)
		return factory.createFdbClusterObject(clusterStatus), nil
	}

	log.Printf("preparing to create fdb cluster: %s/%s", clusterSpec.Namespace, clusterSpec.Name)
	fdbCluster := factory.createFdbClusterObject(clusterSpec)
	err = fdbCluster.Create()
	if err != nil {
		// consider checking k8serrors.IsAlreadyExists(err), but if that's
		// the case, we're probably running concurrently with another
		// test that's using this cluster name -- may as well fail now.
		return nil, err
	}
	// Wait until the cluster CRD object exists. The caller should wait for whatever state they care about.
	fdbCluster.WaitUntilExists()
	// Wait until cluster is reconciled -- otherwise, the operator may not have
	// assigned pods, etc.
	err = fdbCluster.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))
	if err != nil {
		return nil, err
	}

	config.CreationCallback(fdbCluster)

	return fdbCluster, nil
}

func (factory *Factory) ensureHaMemberClusterExists(
	haFdbCluster *HaFdbCluster,
	config *ClusterConfig,
	dcID string,
	seedConnection string,
	databaseConfiguration *fdbv1beta2.DatabaseConfiguration,
) error {
	var initMode bool
	if len(databaseConfiguration.Regions) == 1 {
		initMode = true
	}

	completeDatabaseConfiguration(
		databaseConfiguration,
		databaseConfiguration.RoleCounts,
		databaseConfiguration.StorageEngine,
		databaseConfiguration.RedundancyMode,
	)

	spec := factory.createHaFdbClusterSpec(
		config,
		dcID,
		seedConnection,
		databaseConfiguration,
	)

	curCluster := factory.createFdbClusterObject(spec)
	factory.logClusterInfo(spec)
	// We have to trigger here an update since the cluster already exists!
	fetchedClusterStatus, err := factory.getClusterStatus(config.Name, config.Namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Printf(
				"preparing to create ha fdb cluster: %s/%s",
				curCluster.cluster.Namespace,
				curCluster.cluster.Name,
			)
			err = curCluster.Create()
			if err != nil && !k8serrors.IsAlreadyExists(err) {
				return err
			}
			log.Printf(
				"wait for ha fdb cluster: %s/%s",
				curCluster.cluster.Namespace,
				curCluster.cluster.Name,
			)

			curCluster.WaitUntilExists()
			return haFdbCluster.addCluster(curCluster)
		}
		return err
	}
	fetchedCluster := factory.createFdbClusterObject(fetchedClusterStatus)

	// Cluster already exists, so we want to update it if something is missing. If we call this method for the first time
	// we create a single FDB cluster without a multi-region config, we don't want to overwrite the config if we already
	// have a HA cluster running, since this would change the HA config to a single cluster. We only want to update the
	// database configuration if they have the same number of regions configured. We use the number of regions as a heuristic
	// if the cluster is already running in a HA configuration.
	if !equality.Semantic.DeepEqual(
		fetchedCluster.cluster.Spec.DatabaseConfiguration,
		curCluster.cluster.Spec.DatabaseConfiguration,
	) && !initMode {
		fetchedCluster.cluster.Spec.DatabaseConfiguration = curCluster.cluster.Spec.DatabaseConfiguration
		fetchedCluster.cluster.Spec.SeedConnectionString = seedConnection
		log.Printf("update cluster: %s/%s", curCluster.cluster.Namespace, curCluster.cluster.Name)
		fetchedCluster.UpdateClusterSpec()
	}

	return haFdbCluster.addCluster(fetchedCluster)
}

func (factory *Factory) ensureHAFdbClusterExists(
	config *ClusterConfig,
) *HaFdbCluster {
	fdb := &HaFdbCluster{}
	clusterPrefix := factory.getClusterName()

	databaseConfiguration := config.CreateDatabaseConfiguration()
	dcIDs := GetDcIDsFromConfig(databaseConfiguration)

	initialDatabaseConfiguration := databaseConfiguration.DeepCopy()
	initialDatabaseConfiguration.Regions = []fdbv1beta2.Region{
		{
			DataCenters: []fdbv1beta2.DataCenter{
				{
					ID: dcIDs[0],
				},
			},
		},
	}

	namespaces := factory.MultipleNamespaces(config, dcIDs)
	log.Printf("ensureHAFDBClusterExists namespaces=%s", namespaces)

	newConfig := config.Copy()
	newConfig.Name = fmt.Sprintf("%s-%s", clusterPrefix, dcIDs[0])
	newConfig.Namespace = namespaces[0]

	err := factory.ensureHaMemberClusterExists(
		fdb,
		newConfig,
		dcIDs[0],
		"",
		initialDatabaseConfiguration,
	)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(fdb.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))).
		ToNot(gomega.HaveOccurred())
	log.Printf("primary cluster is reconciled in namespaces=%s", namespaces)

	cluster, err := factory.getClusterStatus(fdb.GetPrimary().Name(), fdb.GetPrimary().Namespace())
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	for idx := range dcIDs {
		currentConfig := config.Copy()
		currentConfig.Name = fmt.Sprintf("%s-%s", clusterPrefix, dcIDs[idx])
		currentConfig.Namespace = namespaces[idx]

		err = factory.ensureHaMemberClusterExists(
			fdb,
			currentConfig,
			dcIDs[idx],
			cluster.Status.ConnectionString,
			&databaseConfiguration,
		)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	// Wait until clusters are ready
	gomega.Expect(fdb.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))).
		ToNot(gomega.HaveOccurred())

	config.CreationCallback(fdb.GetPrimary())

	return fdb
}

// GetDcIDsFromConfig returns  unique DC IDs from the current config.
// TODO (johscheuer): Should this be part of v1beta2?
func GetDcIDsFromConfig(databaseConfiguration fdbv1beta2.DatabaseConfiguration) []string {
	dcSet := map[string]struct{}{}
	dcIDs := make([]string, 0)

	for _, region := range databaseConfiguration.Regions {
		for _, dc := range region.DataCenters {
			if _, ok := dcSet[dc.ID]; ok {
				continue
			}
			dcSet[dc.ID] = struct{}{}

			dcIDs = append(dcIDs, dc.ID)
		}
	}

	return dcIDs
}
