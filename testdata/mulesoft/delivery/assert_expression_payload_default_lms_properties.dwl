 %dw 2.0
import * from dw::test::Asserts
---
payload must equalTo({
  "source": "",
  "user_key": "",
  "webUser": "",
  "GPSTracking": false,
  "orderTrackingInput": [
    {
      "dp": "0413570157",
      "shippingPoint": "7502",
      "soldTo": "0001023614"
    },
    {
      "dp": "0413570158",
      "shippingPoint": "7502",
      "soldTo": "0001023614"
    },
    {
      "dp": "0413570159",
      "shippingPoint": "7502",
      "soldTo": "0001023614"
    },
    {
      "dp": "0413570160",
      "shippingPoint": "7502",
      "soldTo": "0001023614"
    },
    {
      "dp": "0413570156",
      "shippingPoint": "7502",
      "soldTo": "0001023614"
    }
  ]
})