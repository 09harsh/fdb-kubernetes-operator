/*
 * monitor_conf_test.go
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

package internal

import (
	"fmt"
	"strings"

	"k8s.io/utils/ptr"

	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("monitor_conf", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var fakeConnectionString string
	var err error

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).NotTo(HaveOccurred())
		fakeConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
	})

	Context("GetUnifedMonitorConf", func() {
		var baseArgumentLength = 11

		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
		})

		When("there is no connection string", func() {
			It("generates conf with no processes", func() {
				Expect(cluster).NotTo(BeNil())
				cluster.Status.ConnectionString = ""
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.RunServers).NotTo(BeNil())
				Expect(*config.RunServers).To(BeFalse())
				Expect(config.Version).To(Equal(&fdbv1beta2.Versions.Default.Version))
			})
		})

		When("running a storage instance", func() {
			It("generates the conf", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Version).To(Equal(&fdbv1beta2.Versions.Default.Version))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[0],
				).To(Equal(monitorapi.Argument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"}))
				Expect(
					config.Arguments[1],
				).To(Equal(monitorapi.Argument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"}))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=storage"}))
				Expect(
					config.Arguments[4],
				).To(Equal(monitorapi.Argument{Value: "--logdir=/var/log/fdb-trace-logs"}))
				Expect(
					config.Arguments[5],
				).To(Equal(monitorapi.Argument{Value: "--loggroup=" + cluster.Name}))
				Expect(
					config.Arguments[6],
				).To(Equal(monitorapi.Argument{Value: "--datadir=/var/fdb/data"}))
				Expect(
					config.Arguments[7],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameInstanceID,
					},
				}}))
				Expect(
					config.Arguments[8],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_machineid="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameMachineID,
					},
				}}))
				Expect(
					config.Arguments[9],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameZoneID,
					},
				}}))
			})
		})

		When("running a log instance", func() {
			It("generates the conf", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassLog,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Version).To(Equal(&fdbv1beta2.Versions.Default.Version))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=log"}))
			})
		})

		When("using the split image type", func() {
			It("generates the conf", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeSplit,
				)
				Expect(config.Version).To(Equal(&fdbv1beta2.Versions.Default.Version))
				Expect(config.BinaryPath).To(BeEmpty())
				Expect(config.RunServers).To(BeNil())

				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[0],
				).To(Equal(monitorapi.Argument{Value: "--cluster_file=/var/fdb/data/fdb.cluster"}))
				Expect(
					config.Arguments[1],
				).To(Equal(monitorapi.Argument{Value: "--seed_cluster_file=/var/dynamic-conf/fdb.cluster"}))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: ":"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
				Expect(config.Arguments[3]).To(Equal(monitorapi.Argument{Value: "--class=storage"}))
				Expect(
					config.Arguments[4],
				).To(Equal(monitorapi.Argument{Value: "--logdir=/var/log/fdb-trace-logs"}))
				Expect(
					config.Arguments[5],
				).To(Equal(monitorapi.Argument{Value: "--loggroup=" + cluster.Name}))
				Expect(
					config.Arguments[6],
				).To(Equal(monitorapi.Argument{Value: "--datadir=/var/fdb/data"}))
				Expect(
					config.Arguments[7],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameInstanceID,
					},
				}}))
				Expect(
					config.Arguments[8],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_machineid="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameMachineID,
					},
				}}))
				Expect(
					config.Arguments[9],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameZoneID,
					},
				}}))
			})
		})

		When("running multiple processes", func() {
			It("adds a process ID argument", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					2,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(
					config.Arguments[7],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_process_id="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameInstanceID,
					},
					{Value: "-"},
					{ArgumentType: monitorapi.ProcessNumberArgumentType},
				}}))
				Expect(
					config.Arguments[8],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_instance_id="},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNameInstanceID,
					},
				}}))
			})

			It("includes the process number in the data directory", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					2,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments[6]).To(Equal(monitorapi.Argument{
					ArgumentType: monitorapi.ConcatenateArgumentType,
					Values: []monitorapi.Argument{
						{Value: "--datadir=/var/fdb/data/"},
						{ArgumentType: monitorapi.ProcessNumberArgumentType},
					},
				}))
			})
		})

		When("the public IP comes from the pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
			})

			It("does not have a listen address", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
			})
		})

		When("the public IP comes from the service", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
			})

			It("adds a separate listen address", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
				Expect(
					config.Arguments[10],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--listen_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePodIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
			})

			When("some pods do not have the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
				})

				It("does not have a listen address", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength))
					Expect(
						config.Arguments[2],
					).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
						{Value: "--public_address=["},
						{
							ArgumentType: monitorapi.EnvironmentArgumentType,
							Source:       fdbv1beta2.EnvNamePublicIP,
						},
						{Value: "]:"},
						{
							ArgumentType: monitorapi.ProcessNumberArgumentType,
							Offset:       4499,
							Multiplier:   2,
						},
					}}))
				})
			})
		})

		When("TLS is enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes the TLS flag in the address", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4498,
						Multiplier:   2,
					},
					{Value: ":tls"},
				}}))
			})
		})

		Context("with a transition to TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes both addresses", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4498,
						Multiplier:   2,
					},
					{Value: ":tls"},
					{Value: ",["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
			})
		})

		Context("with a transition to non-TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = false
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true
			})

			It("includes both addresses", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments[2],
				).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--public_address=["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4498,
						Multiplier:   2,
					},
					{Value: ":tls"},
					{Value: ",["},
					{
						ArgumentType: monitorapi.EnvironmentArgumentType,
						Source:       fdbv1beta2.EnvNamePublicIP,
					},
					{Value: "]:"},
					{
						ArgumentType: monitorapi.ProcessNumberArgumentType,
						Offset:       4499,
						Multiplier:   2,
					},
				}}))
			})
		})

		When("the cluster has custom parameters", func() {
			When("there are parameters in the general section", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_disable_posix_kernel_aio = 1",
							},
						},
					}
				})

				It("includes the custom parameters", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{
						ArgumentType: monitorapi.ConcatenateArgumentType,
						Values: []monitorapi.Argument{
							{
								ArgumentType: monitorapi.LiteralArgumentType,
								Value:        "--knob_disable_posix_kernel_aio=",
							},
							{
								ArgumentType: monitorapi.LiteralArgumentType,
								Value:        "1",
							},
						}}))
				})
			})

			When("there are parameters on different process classes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_disable_posix_kernel_aio = 1",
							},
						},
						fdbv1beta2.ProcessClassStorage: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_test = test1",
							},
						},
						fdbv1beta2.ProcessClassStateless: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_test = test2",
							},
						},
					}
				})

				It("includes the custom parameters for that class", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{
						ArgumentType: monitorapi.ConcatenateArgumentType,
						Values: []monitorapi.Argument{
							{
								ArgumentType: monitorapi.LiteralArgumentType,
								Value:        "--knob_test=",
							},
							{
								ArgumentType: monitorapi.LiteralArgumentType,
								Value:        "test1",
							},
						}}))
				})
			})

			When("there are parameters in the general section that use the public IP", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"locality_my_fancy_ip = $FDB_PUBLIC_IP",
							},
						},
					}
				})

				It("includes the custom parameters", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
					Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{
						ArgumentType: monitorapi.ConcatenateArgumentType,
						Values: []monitorapi.Argument{
							{
								ArgumentType: monitorapi.LiteralArgumentType,
								Value:        "--locality_my_fancy_ip=",
							},
							{
								ArgumentType: monitorapi.EnvironmentArgumentType,
								Source:       "FDB_PUBLIC_IP",
							},
						}}))
				})

				When("using IPv6 as PodIPFamily", func() {
					BeforeEach(func() {
						cluster.Spec.Routing.PodIPFamily = ptr.To(fdbv1beta2.PodIPFamilyIPv6)
					})

					It("specifies the IP family for the public address", func() {
						config := GetMonitorProcessConfiguration(
							cluster,
							fdbv1beta2.ProcessClassStorage,
							1,
							fdbv1beta2.ImageTypeUnified,
						)
						Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
						Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{
							ArgumentType: monitorapi.ConcatenateArgumentType,
							Values: []monitorapi.Argument{
								{
									ArgumentType: monitorapi.LiteralArgumentType,
									Value:        "--locality_my_fancy_ip=",
								},
								{
									ArgumentType: monitorapi.IPListArgumentType,
									Source:       "FDB_PUBLIC_IP",
									IPFamily:     fdbv1beta2.PodIPFamilyIPv6,
								},
							}}))

						commandLineArgs, err := config.GenerateArguments(1,
							map[string]string{
								"FDB_PUBLIC_IP":   "2001:db8:dead:beef::1,192.168.0.2",
								"FDB_INSTANCE_ID": "test",
								"FDB_MACHINE_ID":  "test",
								"FDB_ZONE_ID":     "test",
								"FDB_DNS_NAME":    "test",
							})
						Expect(err).NotTo(HaveOccurred())
						Expect(
							commandLineArgs[10],
						).To(Equal("--locality_my_fancy_ip=2001:db8:dead:beef::1"))
					})
				})

				When("using IPv4 as PodIPFamily", func() {
					BeforeEach(func() {
						cluster.Spec.Routing.PodIPFamily = ptr.To(fdbv1beta2.PodIPFamilyIPv4)
					})

					It("specifies the IP family for the public address", func() {
						config := GetMonitorProcessConfiguration(
							cluster,
							fdbv1beta2.ProcessClassStorage,
							1,
							fdbv1beta2.ImageTypeUnified,
						)
						Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
						Expect(config.Arguments[10]).To(Equal(monitorapi.Argument{
							ArgumentType: monitorapi.ConcatenateArgumentType,
							Values: []monitorapi.Argument{
								{
									ArgumentType: monitorapi.LiteralArgumentType,
									Value:        "--locality_my_fancy_ip=",
								},
								{
									ArgumentType: monitorapi.IPListArgumentType,
									Source:       "FDB_PUBLIC_IP",
									IPFamily:     fdbv1beta2.PodIPFamilyIPv4,
								},
							}}))

						commandLineArgs, err := config.GenerateArguments(1,
							map[string]string{
								"FDB_PUBLIC_IP":   "2001:db8:dead:beef::1,192.168.0.2",
								"FDB_INSTANCE_ID": "test",
								"FDB_MACHINE_ID":  "test",
								"FDB_ZONE_ID":     "test",
								"FDB_DNS_NAME":    "test",
							})
						Expect(err).NotTo(HaveOccurred())
						Expect(commandLineArgs[10]).To(Equal("--locality_my_fancy_ip=192.168.0.2"))
					})
				})
			})

			When("using IPv6 as PodIPFamily", func() {
				BeforeEach(func() {
					cluster.Spec.Routing.PodIPFamily = ptr.To(fdbv1beta2.PodIPFamilyIPv6)
				})

				It("specifies the IP family for the public address", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength))
					Expect(
						config.Arguments[2],
					).To(Equal(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
						{Value: "--public_address=["},
						{
							ArgumentType: monitorapi.IPListArgumentType,
							Source:       fdbv1beta2.EnvNamePublicIP,
							IPFamily:     6,
						},
						{Value: "]:"},
						{
							ArgumentType: monitorapi.ProcessNumberArgumentType,
							Offset:       4499,
							Multiplier:   2,
						},
					}}))
				})
			})

			When("using IPv4 as PodIPFamily", func() {
				BeforeEach(func() {
					cluster.Spec.Routing.PodIPFamily = ptr.To(fdbv1beta2.PodIPFamilyIPv4)
				})

				It("specifies the IP family for the public address", func() {
					config := GetMonitorProcessConfiguration(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						1,
						fdbv1beta2.ImageTypeUnified,
					)
					Expect(config.Arguments).To(HaveLen(baseArgumentLength))
					Expect(
						config.Arguments,
					).To(ContainElement(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
						{Value: "--public_address=["},
						{
							ArgumentType: monitorapi.IPListArgumentType,
							Source:       fdbv1beta2.EnvNamePublicIP,
							IPFamily:     4,
						},
						{Value: "]:"},
						{
							ArgumentType: monitorapi.ProcessNumberArgumentType,
							Offset:       4499,
							Multiplier:   2,
						},
					}}))
				})
			})
		})

		When("the cluster has an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
			})

			It("uses the variable as the zone ID", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))

				Expect(
					config.Arguments,
				).To(ContainElement(monitorapi.Argument{ArgumentType: monitorapi.ConcatenateArgumentType, Values: []monitorapi.Argument{
					{Value: "--locality_zoneid="},
					{ArgumentType: monitorapi.EnvironmentArgumentType, Source: "RACK"},
				}}))
			})
		})

		When("the spec has custom peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
			})

			It("includes the verification rules", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(
					config.Arguments,
				).To(ContainElement(monitorapi.Argument{Value: "--tls_verify_peers=S.CN=foundationdb.org"}))
			})
		})

		When("the spec has a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
			})

			It("includes the log group", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength))
				Expect(
					config.Arguments,
				).To(ContainElement(monitorapi.Argument{Value: "--loggroup=test-fdb-cluster"}))
			})
		})

		When("the spec has a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
			})

			It("adds an argument for the data center", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(
					config.Arguments,
				).To(ContainElement(monitorapi.Argument{Value: "--locality_dcid=dc01"}))
			})
		})

		When("the spec has a data hall", func() {
			BeforeEach(func() {
				cluster.Spec.DataHall = "dh01"
			})

			It("adds an argument for the data hall", func() {
				config := GetMonitorProcessConfiguration(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					1,
					fdbv1beta2.ImageTypeUnified,
				)
				Expect(config.Arguments).To(HaveLen(baseArgumentLength + 1))
				Expect(
					config.Arguments,
				).To(ContainElement(monitorapi.Argument{Value: "--locality_data_hall=dh01"}))
			})
		})
	})

	Describe("GetStartCommand", func() {
		var pod *corev1.Pod
		var command string
		var address string
		var processClass = fdbv1beta2.ProcessClassStorage
		var processGroupID = "storage-1"

		BeforeEach(func() {
			pod, err = GetPod(cluster, &fdbv1beta2.ProcessGroupStatus{
				ProcessClass:   processClass,
				ProcessGroupID: fdbv1beta2.ProcessGroupID(processGroupID),
			})
			Expect(err).NotTo(HaveOccurred())
			address = pod.Status.PodIP
		})

		When("using the split image", func() {
			BeforeEach(func() {
				imageType := fdbv1beta2.ImageTypeSplit
				cluster.Spec.ImageType = &imageType
			})

			When("no additional custom parameters are defined", func() {
				It("should substitute the variables in the start command", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())

					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})

			When("custom parameters with substitutions are defined", func() {
				It("should substitute the variables in the custom parameters", func() {
					settings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					settings.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{
						"locality_disk_id=$FDB_INSTANCE_ID",
					}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings

					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())

					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf("--locality_disk_id=%s", processGroupID),
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})

			When("multiple storage servers per Pod are defined", func() {
				It("should substitute the variables in the start command", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						2,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())

					id := "storage-1"
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data/1",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						fmt.Sprintf("--locality_instance_id=%s", id),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
						fmt.Sprintf("--locality_process_id=%s-1", id),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))

					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						2,
						2,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data/2",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						fmt.Sprintf("--locality_instance_id=%s", id),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
						fmt.Sprintf("--locality_process_id=%s-2", id),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4503", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})

			When("host replication is used", func() {
				var substitutions map[string]string

				BeforeEach(func() {
					pod.Spec.NodeName = "machine1"
					cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{}

					substitutions, err = GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should provide the host information in the start command", func() {
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						"--locality_instance_id=storage-1",
						"--locality_machineid=machine1",
						"--locality_zoneid=machine1",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})

			When("cross-Kubernetes replication is used", func() {
				var substitutions map[string]string

				BeforeEach(func() {
					pod.Spec.NodeName = "machine1"

					cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
						Key:   "foundationdb.org/kubernetes-cluster",
						Value: "kc2",
					}

					substitutions, err = GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should put the zone ID in the start command", func() {
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						"--locality_instance_id=storage-1",
						"--locality_machineid=machine1",
						"--locality_zoneid=kc2",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})

			When("the binaries from the main container are used", func() {
				var substitutions map[string]string

				BeforeEach(func() {
					cluster.Spec.Version = fdbv1beta2.Versions.Default.String()
					cluster.Status.RunningVersion = fdbv1beta2.Versions.Default.String()
					substitutions, err = GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should include the binary path in the start command", func() {
					id := pod.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--class=storage",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--datadir=/var/fdb/data",
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
						fmt.Sprintf("--locality_instance_id=%s", id),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, id),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, id),
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						fmt.Sprintf("--public_address=%s:4501", address),
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					}, " ")))
				})
			})
		})

		When("using the unified image", func() {
			BeforeEach(func() {
				imageType := fdbv1beta2.ImageTypeUnified
				cluster.Spec.ImageType = &imageType
			})

			It("should generate the unsorted command-line", func() {
				substitutions, err := GetSubstitutionsFromClusterAndPod(
					logr.Discard(),
					cluster,
					pod,
				)
				Expect(err).NotTo(HaveOccurred())
				command, err = GetStartCommandWithSubstitutions(
					cluster,
					processClass,
					substitutions,
					1,
					1,
					cluster.DesiredImageType(),
				)
				Expect(err).NotTo(HaveOccurred())

				Expect(command).To(Equal(strings.Join([]string{
					"/usr/bin/fdbserver",
					"--cluster_file=/var/fdb/data/fdb.cluster",
					"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("--public_address=[%s]:4501", address),
					"--class=storage",
					"--logdir=/var/log/fdb-trace-logs",
					"--loggroup=" + cluster.Name,
					"--datadir=/var/fdb/data/1",
					fmt.Sprintf("--locality_process_id=%s-1", processGroupID),
					fmt.Sprintf("--locality_instance_id=%s", processGroupID),
					fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
					fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
					fmt.Sprintf("--locality_dns_name=%s", substitutions[fdbv1beta2.EnvNameDNSName]),
				}, " ")))
			})

			When("the pod has multiple processes", func() {
				It("should fill in the process number", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						2,
						3,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())

					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("--public_address=[%s]:4503", address),
						"--class=storage",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						"--datadir=/var/fdb/data/2",
						fmt.Sprintf("--locality_process_id=%s-2", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
					}, " ")))
				})
			})

			When("using custom parameters with substitutions", func() {
				BeforeEach(func() {
					settings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					settings.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{
						"locality_disk_id=$FDB_INSTANCE_ID",
						"test=$FDB_MACHINE_ID",
					}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings
				})

				It("should substitute the variables in the custom parameters", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("--public_address=[%s]:4501", address),
						"--class=storage",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						"--datadir=/var/fdb/data/1",
						fmt.Sprintf("--locality_process_id=%s-1", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_disk_id=%s", processGroupID),
						fmt.Sprintf("--test=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
					}, " ")))
				})
			})

			When("using custom parameters with substitutions for dcid locality", func() {
				BeforeEach(func() {
					settings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					settings.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{
						"locality_dcid=$FDB_INSTANCE_ID",
					}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings
					cluster.Spec.DataCenter = "dc"
				})

				It("should substitute the variables in the custom parameters", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("--public_address=[%s]:4501", address),
						"--class=storage",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						"--datadir=/var/fdb/data/1",
						fmt.Sprintf("--locality_process_id=%s-1", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_dcid=%s", processGroupID),
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
					}, " ")))
				})
			})

			When("using custom parameters with substitutions for data_hall locality", func() {
				BeforeEach(func() {
					settings := cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
					settings.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{
						"locality_data_hall=$FDB_INSTANCE_ID",
					}
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral] = settings
					cluster.Spec.DataHall = "data_hall"
				})

				It("should substitute the variables in the custom parameters", func() {
					substitutions, err := GetSubstitutionsFromClusterAndPod(
						logr.Discard(),
						cluster,
						pod,
					)
					Expect(err).NotTo(HaveOccurred())
					command, err = GetStartCommandWithSubstitutions(
						cluster,
						processClass,
						substitutions,
						1,
						1,
						cluster.DesiredImageType(),
					)
					Expect(err).NotTo(HaveOccurred())
					Expect(command).To(Equal(strings.Join([]string{
						"/usr/bin/fdbserver",
						"--cluster_file=/var/fdb/data/fdb.cluster",
						"--seed_cluster_file=/var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("--public_address=[%s]:4501", address),
						"--class=storage",
						"--logdir=/var/log/fdb-trace-logs",
						"--loggroup=" + cluster.Name,
						"--datadir=/var/fdb/data/1",
						fmt.Sprintf("--locality_process_id=%s-1", processGroupID),
						fmt.Sprintf("--locality_instance_id=%s", processGroupID),
						fmt.Sprintf("--locality_machineid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_zoneid=%s-%s", cluster.Name, processGroupID),
						fmt.Sprintf("--locality_data_hall=%s", processGroupID),
						fmt.Sprintf(
							"--locality_dns_name=%s",
							substitutions[fdbv1beta2.EnvNameDNSName],
						),
					}, " ")))
				})
			})
		})
	})

	Describe("GetMonitorConf", func() {
		var conf string
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
		})

		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with a test instance", func() {
			BeforeEach(func() {
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassTest,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the test conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = test",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with DNS names in locality fields disabled", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = ptr.To(false)
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
				}, "\n")))
			})
		})

		Context("with a basic storage instance with multiple storage servers per Pod", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf with two processes", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/1",
					fmt.Sprintf("locality_process_id = $%s-1", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
					"[fdbserver.2]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4503", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data/2",
					fmt.Sprintf("locality_process_id = $%s-2", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with the public IP from the pod", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				conf, err = GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with the public IP from the service", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				cluster.Status.HasListenIPsForAllPods = true
				conf, err = GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should generate the storage conf", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("listen_address = $%s:4501", fdbv1beta2.EnvNamePodIP),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})

			Context("with pods without the listen IP environment variable", func() {
				BeforeEach(func() {
					cluster.Status.HasListenIPsForAllPods = false
					conf, err = GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should generate the storage conf", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
						fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
						fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
						fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
					}, "\n")))
				})
			})
		})

		Context("with TLS enabled", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = false
				cluster.Status.RequiredAddresses.TLS = true
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the TLS flag in the address", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4500:tls", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with a transition to TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = true
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf(
						"public_address = $%s:4500:tls,$%s:4501",
						fdbv1beta2.EnvNamePublicIP,
						fdbv1beta2.EnvNamePublicIP,
					),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with a transition to non-TLS", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.EnableTLS = false
				cluster.Status.RequiredAddresses.NonTLS = true
				cluster.Status.RequiredAddresses.TLS = true

				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include both addresses", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf(
						"public_address = $%s:4500:tls,$%s:4501",
						fdbv1beta2.EnvNamePublicIP,
						fdbv1beta2.EnvNamePublicIP,
					),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with custom parameters", func() {
			Context("with general parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_disable_posix_kernel_aio = 1",
							},
						},
					}
					conf, err = GetMonitorConf(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						nil,
						cluster.GetStorageServersPerPod(),
					)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should include the custom parameters", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
						fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
						fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
						"knob_disable_posix_kernel_aio = 1",
						fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
					}, "\n")))
				})
			})

			Context("with process-class parameters", func() {
				BeforeEach(func() {
					cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
						fdbv1beta2.ProcessClassGeneral: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_disable_posix_kernel_aio = 1",
							},
						},
						fdbv1beta2.ProcessClassStorage: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_test = test1",
							},
						},
						fdbv1beta2.ProcessClassStateless: {
							CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
								"knob_test = test2",
							},
						},
					}
					conf, err = GetMonitorConf(
						cluster,
						fdbv1beta2.ProcessClassStorage,
						nil,
						cluster.GetStorageServersPerPod(),
					)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should include the custom parameters", func() {
					Expect(conf).To(Equal(strings.Join([]string{
						"[general]",
						"kill_on_configuration_change = false",
						"restart_delay = 60",
						"[fdbserver.1]",
						fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
						"cluster_file = /var/fdb/data/fdb.cluster",
						"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
						fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
						"class = storage",
						"logdir = /var/log/fdb-trace-logs",
						"loggroup = " + cluster.Name,
						"datadir = /var/fdb/data",
						fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
						fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
						fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
						"knob_test = test1",
						fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
					}, "\n")))
				})
			})
		})

		Context("with an alternative fault domain variable", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the variable as the zone ID", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					"locality_zoneid = $RACK",
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with peer verification rules", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.PeerVerificationRules = "S.CN=foundationdb.org"
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the verification rules", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					"tls_verify_peers = S.CN=foundationdb.org",
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with a custom log group", func() {
			BeforeEach(func() {
				cluster.Spec.LogGroup = "test-fdb-cluster"
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				Expect(conf).To(Equal(strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = test-fdb-cluster",
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")))
			})
		})

		Context("with a data center", func() {
			BeforeEach(func() {
				cluster.Spec.DataCenter = "dc01"
				conf, err = GetMonitorConf(
					cluster,
					fdbv1beta2.ProcessClassStorage,
					nil,
					cluster.GetStorageServersPerPod(),
				)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the log group", func() {
				t := strings.Join([]string{
					"[general]",
					"kill_on_configuration_change = false",
					"restart_delay = 60",
					"[fdbserver.1]",
					fmt.Sprintf("command = $%s/fdbserver", fdbv1beta2.EnvNameBinaryDir),
					"cluster_file = /var/fdb/data/fdb.cluster",
					"seed_cluster_file = /var/dynamic-conf/fdb.cluster",
					fmt.Sprintf("public_address = $%s:4501", fdbv1beta2.EnvNamePublicIP),
					"class = storage",
					"logdir = /var/log/fdb-trace-logs",
					"loggroup = " + cluster.Name,
					"datadir = /var/fdb/data",
					fmt.Sprintf("locality_instance_id = $%s", fdbv1beta2.EnvNameInstanceID),
					fmt.Sprintf("locality_machineid = $%s", fdbv1beta2.EnvNameMachineID),
					fmt.Sprintf("locality_zoneid = $%s", fdbv1beta2.EnvNameZoneID),
					"locality_dcid = dc01",
					fmt.Sprintf("locality_dns_name = $%s", fdbv1beta2.EnvNameDNSName),
				}, "\n")
				Expect(conf).To(Equal(t))
			})
		})
	})

})
