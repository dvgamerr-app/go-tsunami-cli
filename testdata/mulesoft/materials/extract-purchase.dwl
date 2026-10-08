%dw 2.0
output application/json
var data = vars.DATA
var purchasings = data.purchasings default []
---
purchasings map (item, index) -> {
	"materialCode": data.materialCode,
	"plant": item.plant,
	"plantName": item.plantName,
	"purchaseGroup": item.purchaseGroup,
	"purchaseGroupDesc": item.purchaseGroupDesc
}