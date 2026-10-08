%dw 2.0
output application/json
fun nullIfEmpty(value) = if (!isEmpty(value)) value else null
---
payload groupBy ((item, i) -> "$(item.id)|$(item.typeCode)") pluck ((value,key,i) -> {
   id: nullIfEmpty(value[0].id),
   typeCode: nullIfEmpty(value[0].typeCode),
   "type": nullIfEmpty(value[0]."type"),
   name: nullIfEmpty(value[0].nameEN),
   children: value map ({
       id: nullIfEmpty($.childNameTH),
       nameEN: nullIfEmpty($.childNameEN),
       nameTH: nullIfEmpty($.childNameTH),
   })
})