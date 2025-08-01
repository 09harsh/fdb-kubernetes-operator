/*
 * update_config_map.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// UpdateConfigMap provides a reconciliation step for updating the dynamic config
// for a cluster.
type updateConfigMap struct{}

// reconcile runs the reconciler's work.
func (u updateConfigMap) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	_ *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	configMap, err := internal.GetConfigMap(cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(
		ctx,
		types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name},
		existing,
	)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.V(1).Info("Creating config map", "name", configMap.Name)
			err = r.Create(ctx, configMap)
			if err != nil {
				return &requeue{curError: err}
			}
			return nil
		}

		return &requeue{curError: err}
	}

	metadataCorrect := !internal.MergeLabels(&existing.ObjectMeta, configMap.ObjectMeta)
	if internal.MergeAnnotations(&existing.ObjectMeta, configMap.ObjectMeta) {
		metadataCorrect = false
	}

	if !equality.Semantic.DeepEqual(existing.Data, configMap.Data) || !metadataCorrect {
		logger.Info("Updating config map")
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "UpdatingConfigMap", "")
		existing.Data = configMap.Data
		err = r.Update(ctx, existing)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	return nil
}
