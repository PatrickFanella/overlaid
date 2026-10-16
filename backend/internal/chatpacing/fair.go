package chatpacing

import "git.subcult.tv/PatrickFanella/over-laid/internal/permitturns"

type FairRequest = permitturns.Request

var ErrFairRequired = permitturns.ErrRequired
var ErrFairState = permitturns.ErrState
var decodeFair = permitturns.Decode

const fairTenantLimit = permitturns.TenantLimit
