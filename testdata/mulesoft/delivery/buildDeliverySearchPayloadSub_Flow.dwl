%dw 2.0
output application/json
var orderTrackingInput = (payload.delHeaderData.data default []) map ((item, index) ->{
    "dp":item.dpNo,
    "shippingPoint":item.shippingPoint,
    "soldTo":item.soldTo
} )

var source = vars.queryParams.source default ""
var user_key = vars.queryParams.userKey default ""
var webUser = vars.queryParams.webUser default ""
var gpsTracking = vars.queryParams.GPSTracking default false
---
{
  "source": source,
  "user_key": user_key,
  "webUser": webUser,
  "GPSTracking": gpsTracking,
  "orderTrackingInput": orderTrackingInput
}