%dw 2.0
output application/json
---
[{
	"materialCode": payload.data.materialCode,
	"materialGroup": payload.data.materialGroup,
	"materialDescTh": payload.data.materialDescTh,
	"materialDescEn": payload.data.materialDescEn,
	"materialType": payload.data.materialType,
	"materialTypeDesc": payload.data.materialTypeDesc,
	"baseUnit": payload.data.baseUnit,
	"baseUnitDesc": payload.data.baseUnitDesc,
	"deleteFlag": payload.data.deleteFlag
}]