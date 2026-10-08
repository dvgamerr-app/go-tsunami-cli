%dw 2.0
output application/json
var data = vars.DATA
var materialClass = data.materialClass
---
[{
	"materialCode": data.materialCode,
	"grade": materialClass.grade,
	"basisWeight": materialClass.basisWeight,
	"rollWidth": materialClass.rollWidth,
	"diameter": materialClass.diameter,
	"rollLength": materialClass.rollLength,
	"coreSize": materialClass.coreSize
}]