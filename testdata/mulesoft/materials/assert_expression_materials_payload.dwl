%dw 2.0
import * from dw::test::Asserts
---
payload must equalTo([
  {
    "materialCode": "Z02CAV125M1080117N",
    "materialGroup": "TG00",
    "materialDescTh": "CAV 125M-108 DIA117N",
    "materialDescEn": "CAV 125M-108 DIA117N",
    "materialType": "82",
    "materialTypeDesc": "Trading Goods",
    "baseUnit": "KG",
    "baseUnitDesc": "Kilogram",
    "deleteFlag": null
  }
])