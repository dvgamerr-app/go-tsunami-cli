%dw 2.0
output application/json
var data = vars.DATA
var plantStorages = data.plantStorages default []
---
plantStorages map (item, index) -> {
	"materialCode": data.materialCode,
	"plant": item.plant,
	"sloc": item.sloc,
	"slocDesc": item.slocDesc,
	"unitIssue": item.unitIssue
}