/*
 * stop_backup.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// stopBackup provides a reconciliation step for stopping backup.
type stopBackup struct {
}

// reconcile runs the reconciler's work.
func (s stopBackup) reconcile(
	ctx context.Context,
	r *FoundationDBBackupReconciler,
	backup *fdbv1beta2.FoundationDBBackup,
) *requeue {
	if backup.ShouldRun() || backup.Status.BackupDetails == nil ||
		!backup.Status.BackupDetails.Running {
		return nil
	}

	adminClient, err := r.adminClientForBackup(ctx, backup)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	err = adminClient.StopBackup(backup.BackupURL())
	if err != nil {
		return &requeue{curError: err}
	}

	return nil
}
