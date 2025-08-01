/*
 * exclude_processes.go
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
	"fmt"
	"net"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/coordination"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/coordinator"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

// excludeProcesses provides a reconciliation step for excluding processes from
// the database.
type excludeProcesses struct{}

// excludeEntry represents an entry for a process group that should be excluded and all the associated addresses.
type excludeEntry struct {
	processGroupID fdbv1beta2.ProcessGroupID
	addresses      []fdbv1beta2.ProcessAddress
}

// reconcile runs the reconciler's work.
func (e excludeProcesses) reconcile(
	ctx context.Context,
	r *FoundationDBClusterReconciler,
	cluster *fdbv1beta2.FoundationDBCluster,
	status *fdbv1beta2.FoundationDBStatus,
	logger logr.Logger,
) *requeue {
	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	adminClient.WithValues()
	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	exclusions, err := fdbstatus.GetExclusions(status)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}
	logger.Info("current exclusions", "exclusions", exclusions)
	pendingExclusions := map[fdbv1beta2.ProcessGroupID]time.Time{}
	updatePendingExclusions := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		pendingExclusions, err = adminClient.GetPendingForExclusion(
			cluster.Spec.ProcessGroupIDPrefix,
		)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	fdbProcessesToExcludeByClass, ongoingExclusionsByClass := getProcessesToExclude(
		exclusions,
		cluster,
		pendingExclusions,
		updatePendingExclusions,
	)

	// No processes have to be excluded we can directly return.
	if len(fdbProcessesToExcludeByClass) == 0 {
		return nil
	}

	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		err := adminClient.UpdatePendingForExclusion(updatePendingExclusions)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	// We need the information below to check if the excluded processes are coordinators to make sure we can change the
	// coordinators before doing the exclusion.
	coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
	coordinatorsExclusionString := map[string]fdbv1beta2.None{}
	coordinatorsAddress := map[string]fdbv1beta2.None{}
	for _, processGroup := range cluster.Status.ProcessGroups {
		if _, ok := coordinators[string(processGroup.ProcessGroupID)]; !ok {
			continue
		}

		coordinatorsExclusionString[processGroup.GetExclusionString()] = fdbv1beta2.None{}

		for _, addr := range processGroup.Addresses {
			coordinatorsAddress[addr] = fdbv1beta2.None{}
		}
	}

	// Make sure it's safe to exclude processes.
	err = fdbstatus.CanSafelyExcludeProcessesWithRecoveryState(
		cluster,
		status,
		r.MinimumRecoveryTimeForExclusion,
	)
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true, delay: 10 * time.Second}
	}

	var fdbProcessesToExclude []fdbv1beta2.ProcessAddress
	desiredProcesses, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	readyExclusions := map[fdbv1beta2.ProcessGroupID]time.Time{}
	updateReadyExclusions := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		readyExclusions, err = adminClient.GetReadyForExclusion(cluster.Spec.ProcessGroupIDPrefix)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}
	// transactionSystemExclusionAllowed will keep track if the exclusion is allowed and if the operator is allowed to
	// exclude processes from the transaction system. If multiple processes from different processes classes that are part
	// of the transaction system should be excluded, the operator will expect that the exclusion is allowed for all
	// transaction system processes. The idea here is to reduce the number of recoveries during transaction system
	// migrations as the stateless pods are often created much faster than the log pod as the stateless pods don't have
	// to wait for the storage provisioning.
	transactionSystemExclusionAllowed := true
	allProcessesExcluded := true
	desiredProcessesMap := desiredProcesses.Map()
	for processClass := range fdbProcessesToExcludeByClass {
		contextLogger := logger.WithValues("processClass", processClass)
		ongoingExclusions := ongoingExclusionsByClass[processClass]
		processesToExclude := fdbProcessesToExcludeByClass[processClass]

		allowedExclusions, missingProcesses := getAllowedExclusionsAndMissingProcesses(
			contextLogger,
			cluster,
			processClass,
			desiredProcessesMap[processClass],
			ongoingExclusions,
			r.SimulationOptions.SimulateTime,
		)
		if allowedExclusions <= 0 {
			if processClass.IsTransaction() {
				transactionSystemExclusionAllowed = false
			}
			contextLogger.Info(
				"Waiting for missing processes before continuing with the exclusion",
				"missingProcesses",
				missingProcesses,
				"addressesToExclude",
				processesToExclude,
				"allowedExclusions",
				allowedExclusions,
				"ongoingExclusions",
				ongoingExclusions,
			)
			allProcessesExcluded = false
			continue
		}

		// If we are not able to exclude all processes at once print a log message.
		if len(processesToExclude) > allowedExclusions {
			allProcessesExcluded = false
			contextLogger.Info(
				"Some processes are still missing but continuing with the exclusion",
				"missingProcesses",
				missingProcesses,
				"addressesToExclude",
				processesToExclude,
				"allowedExclusions",
				allowedExclusions,
				"ongoingExclusions",
				ongoingExclusions,
			)
		}

		if len(processesToExclude) < allowedExclusions {
			allowedExclusions = len(processesToExclude)
		}

		// Add as many processes as allowed to the exclusion list. The allowedExclusions reflects the count of processes
		// that can be excluded, that could also be multiple addresses.
		var exclusionIdx int
		for exclusionIdx < allowedExclusions {
			entry := processesToExclude[exclusionIdx]
			if _, ok := readyExclusions[entry.processGroupID]; !ok {
				updateReadyExclusions[entry.processGroupID] = fdbv1beta2.UpdateActionAdd
			}
			fdbProcessesToExclude = append(fdbProcessesToExclude, entry.addresses...)
			exclusionIdx++
		}
	}

	if len(fdbProcessesToExclude) == 0 {
		return &requeue{
			message:        "more exclusions needed but not allowed, have to wait for new processes to come up",
			delayedRequeue: true,
		}
	}

	// In case that there are processes from different transaction process classes, we expect that the operator is allowed
	// to exclude processes from all the different process classes. If not the operator will delay the exclusion.
	if !transactionSystemExclusionAllowed {
		return &requeue{
			message:        "more exclusions needed but not allowed, have to wait until new processes for the transaction system are up to reduce number of recoveries.",
			delayedRequeue: true,
		}
	}

	// Update the ready for exclusion entries if some are ready.
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		err = adminClient.UpdateReadyForExclusion(updateReadyExclusions)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	// Make sure the exclusions are coordinated across multiple operator instances.
	err = r.takeLock(logger, cluster, "exclude processes")
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	// In case of the global synchronization mode we have to perform some additional checks.
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		// Fetching all pending exclusions.
		pendingExclusions, err = adminClient.GetPendingForExclusion("")
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		// Fetching all ready for exclusions.
		readyExclusions, err = adminClient.GetReadyForExclusion("")
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		// Check if all processes can be excluded, or if only a subset of processes can be excluded.
		var allowedExclusions map[fdbv1beta2.ProcessGroupID]time.Time
		allowedExclusions, err = coordination.AllProcessesReadyForExclusion(
			logger,
			pendingExclusions,
			readyExclusions,
			r.GlobalSynchronizationWaitDuration,
		)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}

		if len(allowedExclusions) != len(readyExclusions) {
			allProcessesExcluded = false
		}

		// Convert all the process groups that should be excluded to the right addresses based on the cluster status.
		useLocalities := cluster.UseLocalitiesForExclusion()
		fdbProcessesToExclude, err = coordination.GetAddressesFromCoordinationState(
			logger,
			adminClient,
			allowedExclusions,
			useLocalities,
			!useLocalities,
		)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	r.Recorder.Event(
		cluster,
		corev1.EventTypeNormal,
		"ExcludingProcesses",
		fmt.Sprintf("Excluding %v", fdbProcessesToExclude),
	)
	// We use the no_wait exclusion here to trigger the exclusion without waiting for the data movement to complete.
	// There is no need to wait for the data movement to complete in this call as later calls will verify that the
	// data is moved and the processes are fully excluded. Using the no_wait flag here will reduce the timeout errors
	// as those are hit most of the time if at least one storage process is included in the exclusion list.
	err = adminClient.ExcludeProcessesWithNoWait(fdbProcessesToExclude, true)
	// Reset the SecondsSinceLastRecovered since the operator just excluded some processes, which will cause a recovery.
	status.Cluster.RecoveryState.SecondsSinceLastRecovered = 0.0
	// If the exclusion failed, we don't want to change the coordinators and delay the coordinators change to a later time.
	if err != nil {
		return &requeue{curError: err, delayedRequeue: true}
	}

	var coordinatorExcluded bool
	for _, excludeProcess := range fdbProcessesToExclude {
		excludeString := excludeProcess.String()
		_, excludedLocality := coordinatorsExclusionString[excludeString]
		_, excludedAddress := coordinatorsAddress[excludeString]

		if excludedAddress || excludedLocality {
			logger.Info(
				"process to be excluded is also a coordinator",
				"excludeProcess",
				excludeProcess.String(),
			)
			coordinatorExcluded = true
		}
	}

	// Only if a coordinator was excluded we have to check for an error and update the cluster.
	if coordinatorExcluded {
		// If a coordinator should be excluded, we will change the coordinators directly after the exclusion.
		// This should reduce the observed recoveries, see: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/2018.
		coordinatorErr := coordinator.ChangeCoordinators(logger, adminClient, cluster, status)
		if coordinatorErr != nil {
			return &requeue{curError: coordinatorErr, delayedRequeue: true}
		}

		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err, delayedRequeue: true}
		}
	}

	// If not all processes are excluded, ensure we requeue after 5 minutes.
	if !allProcessesExcluded {
		return &requeue{
			message:        "Additional processes must be excluded",
			delay:          5 * time.Minute,
			delayedRequeue: true,
		}
	}

	return nil
}

func getProcessesToExclude(
	exclusions []fdbv1beta2.ProcessAddress,
	cluster *fdbv1beta2.FoundationDBCluster,
	pendingExclusions map[fdbv1beta2.ProcessGroupID]time.Time,
	updatePendingExclusions map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction,
) (map[fdbv1beta2.ProcessClass][]excludeEntry, map[fdbv1beta2.ProcessClass]int) {
	fdbProcessesToExcludeByClass := make(map[fdbv1beta2.ProcessClass][]excludeEntry)
	// This map keeps track on how many processes are currently excluded but haven't finished the exclusion yet.
	ongoingExclusionsByClass := make(map[fdbv1beta2.ProcessClass]int)

	currentExclusionMap := make(map[string]fdbv1beta2.None, len(exclusions))
	for _, exclusion := range exclusions {
		currentExclusionMap[exclusion.String()] = fdbv1beta2.None{}
	}

	for _, processGroup := range cluster.Status.ProcessGroups {
		// Tester processes must not be excluded as they are a special role.
		if processGroup.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}
		// Ignore process groups that are not marked for removal.
		if !processGroup.IsMarkedForRemoval() {
			continue
		}

		// Ignore all process groups that are already marked as fully excluded.
		if processGroup.IsExcluded() {
			continue
		}

		// Process already excluded using locality, so we don't have to exclude it again.
		if _, ok := currentExclusionMap[processGroup.GetExclusionString()]; ok {
			ongoingExclusionsByClass[processGroup.ProcessClass]++
			continue
		}

		if _, ok := pendingExclusions[processGroup.ProcessGroupID]; !ok {
			updatePendingExclusions[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
		}

		// We are excluding process here using the locality field. It might be possible that the process was already excluded using IP before
		// but for the sake of consistency it is better to exclude process using locality as well.
		if cluster.UseLocalitiesForExclusion() {
			// Already excluded, so we don't have to exclude it again.
			if _, ok := currentExclusionMap[processGroup.GetExclusionString()]; ok {
				continue
			}

			if len(fdbProcessesToExcludeByClass[processGroup.ProcessClass]) == 0 {
				fdbProcessesToExcludeByClass[processGroup.ProcessClass] = append(
					fdbProcessesToExcludeByClass[processGroup.ProcessClass],
					excludeEntry{
						processGroupID: processGroup.ProcessGroupID,
						addresses: []fdbv1beta2.ProcessAddress{
							{StringAddress: processGroup.GetExclusionString()},
						},
					},
				)
				continue
			}

			fdbProcessesToExcludeByClass[processGroup.ProcessClass] = append(
				fdbProcessesToExcludeByClass[processGroup.ProcessClass],
				excludeEntry{
					processGroupID: processGroup.ProcessGroupID,
					addresses: []fdbv1beta2.ProcessAddress{
						{StringAddress: processGroup.GetExclusionString()},
					},
				},
			)
			continue
		}

		allAddressesExcluded := true
		entry := excludeEntry{
			processGroupID: processGroup.ProcessGroupID,
			addresses:      []fdbv1beta2.ProcessAddress{},
		}

		var addresses []fdbv1beta2.ProcessAddress
		for _, address := range processGroup.Addresses {
			// Already excluded, so we don't have to exclude it again.
			if _, ok := currentExclusionMap[address]; ok {
				continue
			}

			allAddressesExcluded = false
			addresses = append(
				addresses,
				fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(address)},
			)
		}

		if len(addresses) > 0 {
			entry.addresses = addresses
			fdbProcessesToExcludeByClass[processGroup.ProcessClass] = append(
				fdbProcessesToExcludeByClass[processGroup.ProcessClass],
				entry,
			)
		}

		// Only if all known addresses are excluded we assume this is an ongoing exclusion. Otherwise, it might be that
		// the Pod was recreated and got a new IP address assigned.
		if allAddressesExcluded {
			ongoingExclusionsByClass[processGroup.ProcessClass]++
		}
	}

	return fdbProcessesToExcludeByClass, ongoingExclusionsByClass
}

// getAllowedExclusionsAndMissingProcesses will check if new processes for the specified process class can be excluded. The calculation takes
// the current ongoing exclusions into account and the desired process count. If there are process groups that have
// the MissingProcesses condition this method will forbid exclusions until all process groups with this condition have
// this condition for longer than ignoreMissingProcessDuration. The idea behind this is to try to exclude as many processes
// at once e.g. to reduce the number of recoveries and data movement.
func getAllowedExclusionsAndMissingProcesses(
	logger logr.Logger,
	cluster *fdbv1beta2.FoundationDBCluster,
	processClass fdbv1beta2.ProcessClass,
	desiredProcessCount int,
	ongoingExclusions int,
	inSimulation bool,
) (int, []fdbv1beta2.ProcessGroupID) {
	// Block excludes on missing processes not marked for removal unless they are missing for a long time and the process might be broken
	// or the namespace quota was hit.
	missingProcesses := make([]fdbv1beta2.ProcessGroupID, 0)
	var validProcesses int

	exclusionsAllowed := true
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.ProcessClass != processClass {
			continue
		}

		// Those should already be filtered out by the previous method.
		if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
			continue
		}

		missingTimestamp := processGroup.GetConditionTime(fdbv1beta2.MissingProcesses)
		if missingTimestamp != nil && !inSimulation {
			missingTime := time.Unix(*missingTimestamp, 0)
			missingProcesses = append(missingProcesses, processGroup.ProcessGroupID)
			logger.V(1).
				Info("Missing processes", "processGroupID", processGroup.ProcessGroupID, "missingTime", missingTime.String())

			if time.Since(missingTime) < coordination.IgnoreMissingProcessDuration {
				exclusionsAllowed = false
			}
			continue
		}

		validProcesses++
	}

	if !exclusionsAllowed {
		logger.Info(
			"Found at least one missing process, that was not missing for more than 5 minutes",
			"missingProcesses",
			missingProcesses,
		)
		return 0, missingProcesses
	}

	return getAllowedExclusions(
		logger,
		validProcesses,
		desiredProcessCount,
		ongoingExclusions,
		cluster.DesiredFaultTolerance(),
	), missingProcesses
}

// getAllowedExclusions will return the number of allowed exclusions. If no exclusions are allowed this method will return a 0.
// The assumption here is that we will only exclude a process if there is a replacement ready for it. We add the desired fault
// tolerance to have some buffer to prevent cases where the operator might need to exclude more processes but there are more
// missing processes.
func getAllowedExclusions(
	logger logr.Logger,
	validProcesses int,
	desiredProcessCount int,
	ongoingExclusions int,
	faultTolerance int,
) int {
	logger.V(1).
		Info("getAllowedExclusions", "validProcesses", validProcesses, "desiredProcessCount", desiredProcessCount, "ongoingExclusions", ongoingExclusions, "faultTolerance", faultTolerance)
	allowedExclusions := validProcesses + faultTolerance - desiredProcessCount - ongoingExclusions
	if allowedExclusions < 0 {
		return 0
	}

	return allowedExclusions
}
