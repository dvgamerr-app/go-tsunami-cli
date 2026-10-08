 %dw 2.0
import * from dw::test::Asserts
---
payload must equalTo({
  "source": "KOS",
  "user_key": "test_gpstracking",
  "webUser": "123",
  "GPSTracking": false,
  "orderTrackingInput": []
})