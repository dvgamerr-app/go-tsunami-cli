%dw 2.0
import * from dw::test::Asserts
---
payload must equalTo([
  {
    "materialCode": "Z02CAV125M1080117N",
    "plant": "7531",
    "plantName": "[SD] SKICBP Banpong (Purc)",
    "purchaseGroup": "C03",
    "purchaseGroupDesc": "Ekapod C."
  },
  {
    "materialCode": "Z02CAV125M1080117N",
    "plant": "7533",
    "plantName": "[SD] SKICBP Wangsala (Purc)",
    "purchaseGroup": "C03",
    "purchaseGroupDesc": "Ekapod C."
  },
  {
    "materialCode": "Z02CAV125M1080117N",
    "plant": "7546",
    "plantName": "[SD] SKICBP ESC LCB",
    "purchaseGroup": "C03",
    "purchaseGroupDesc": "Ekapod C."
  }
])