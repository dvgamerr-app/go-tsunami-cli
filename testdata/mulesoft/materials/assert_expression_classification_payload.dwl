%dw 2.0
import * from dw::test::Asserts
---
payload must equalTo([
  {
    "materialCode": "Z02CAV125M1080117N",
    "grade": "CAV",
    "basisWeight": "125",
    "rollWidth": "108.00 cm",
    "diameter": "117.0 cm",
    "rollLength": null,
    "coreSize": "3.000 in"
  }
])