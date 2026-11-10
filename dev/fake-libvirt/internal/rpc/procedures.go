// SPDX-FileCopyrightText: 2026 cobaltcore-dev contributors
//
// SPDX-License-Identifier: Apache-2.0

package rpc

const (
	ProcConnectOpen                             = 1
	ProcConnectClose                            = 2
	ProcConnectGetVersion                       = 4
	ProcDomainGetXMLDesc                        = 14
	ProcDomainGetAutostart                      = 15
	ProcDomainGetInfo                           = 16
	ProcDomainGetOsType                         = 19
	ProcDomainLookupByName                      = 23
	ProcConnectSupportsFeature                  = 60
	ProcAuthList                                = 66
	ProcConnectGetUri                           = 110
	ProcNodeGetSecurityModel                    = 122
	ProcConnectGetLibVersion                    = 157
	ProcDomainIsPersistent                      = 151
	ProcDomainHasManagedSave                    = 183
	ProcDomainGetState                          = 212
	ProcDomainGetJobStats                       = 298
	ProcConnectListAllDomains                   = 273
	ProcConnectDomainEventCallbackRegisterAny   = 316
	ProcConnectDomainEventCallbackDeregisterAny = 317
	ProcDomainEventCallbackLifecycle            = 318
	ProcConnectRegisterCloseCallback            = 360
	ProcDomainGetAutostartOnce                  = 449
	ProcDomainGetMessages                       = 426
)
