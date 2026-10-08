%dw 2.0
output application/json

fun QueryTotalBuilder (tableName, where) = [
		"SELECT COUNT(*) total",
		"FROM $(tableName)",
		if (sizeOf(flatten(where)) > 0) "WHERE $(flatten(where) joinBy " AND ")" else "",
] joinBy "\n" as String

fun QueryDataBuilder (tableName: String, column: Array<String>, where: Array<String>, param: Any) = do {
	var orderBy = "ORDER BY $(upper(param.order_by as String replace ":" with(" ")))"
	var offsetRow = "OFFSET $(param.offset as String) ROWS $(if (param.limit > 0) "FETCH FIRST $(param.limit as String) ROWS ONLY" else "")"
	---
	[
		"SELECT",
		column joinBy "\n  ",
		"FROM $(tableName)",
		if (sizeOf(flatten(where)) > 0) "WHERE $(flatten(where) joinBy " AND ")" else "",
		orderBy,
		offsetRow as String
	] joinBy "\n" as String
}

var columnName = [
  "BP_ID [refId],",
  "SRC_SYS [channel],",
  "DEALER_NAME [name],",
  "BP_NO [bpId],",
  "seller_status [status],",
  "address [address],",
  "DISTRICT [subDistrict],",
  "PROVINCE [province],",
	"POSTAL_CODE [postCode],",
  "REGION_NAME [region],",
  "OPENING_HOURS [openingHours],",
  "OPENING_HOURS_SPECIAL [openingHoursSpecial],",
  "TELEPHONE [phones],",
  "LOCATION_LINK [mapUrl],",
  "MAP_FILE [mapFileUrl],",
  "CONTACT [contact],",
  "SHOP_STATUS_TEXT [noteStatus],",
  "SHOP_OPTIONAL [textsecondaryStore],",
  "TRAVEL [directionTip],",
  "SELLER_REGION_CODE [saleAreaCode],",
  "SELLER_PRICE_GROUP [saleGroupCode],",
  "CUSTOMER_GROUP_CODE [customerGroupCode],",
  "CUSTOMER_GROUP_NAME [customerGroupName],",
  "FORMAT(CREATED_DT, 'yyyy-MM-ddTHH:mm:ss.fffZ') [createdAt],",
  "FORMAT(UPDATED_DT, 'yyyy-MM-ddTHH:mm:ss.fffZ') [updatedAt]",
]

var param = vars.query_param

var whereQuery = [
	if (!isEmpty(param.search)) "($([
		"DEALER_NAME LIKE :search",
		"address LIKE :search",
		"DISTRICT LIKE :search",
		"PROVINCE LIKE :search",
		"REGION_NAME LIKE :search",
	] joinBy " OR "))" else [],
	if (!isEmpty(param.name)) [ "DEALER_NAME LIKE :name" ] else [],
	if (!isEmpty(param.channels)) "SRC_SYS IN ($(param.channels))" else [],
	if (!isEmpty(param.area_names)) "REGION_NAME IN ($(param.area_names))" else [],
	if (!isEmpty(param.province_names)) "PROVINCE IN ($(param.province_names))" else [],
]
---
{
	total: QueryTotalBuilder("ODSDWHAPI.COMN_SELLER", whereQuery),
	data: QueryDataBuilder("ODSDWHAPI.COMN_SELLER", columnName, whereQuery, param)
}