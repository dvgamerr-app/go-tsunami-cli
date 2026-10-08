%dw 2.0
output application/json
import paramSourceChannel, paramOrderBy, sqlEscape from ParamNormalizer

var limit = 10
var offset = 0

var params = attributes.queryParams

var cat_codes = params.*cat_codes joinBy "," splitBy ","
var brand_names = params.*brand_names joinBy "," splitBy ","
var ids = params.*ids joinBy "," splitBy ","

var order_by = paramOrderBy({
	"updated_at": "Modified_Date",
	"created_at": "Created_Date",
	"name": "Product_name_TH,Product_name_EN",
	"id": "PROD_ID",
	"cms_id": "CMS_ID",
}, "updated_at:desc", "id:asc", params.*order_by)
---
{
	search: if (isEmpty(params.search)) null else "%$(params.search as String)%",
	search_name: if (isEmpty(params.search_name)) null else "%$(params.search_name as String)%",
	search_brand: if (isEmpty(params.search_brand)) null else "%$(params.search_brand as String)%",
	search_series: if (isEmpty(params.search_series)) null else "%$(params.search_series as String)%",
	search_color: if (isEmpty(params.search_color)) null else "%$(params.search_color as String)%",
	search_bu: if (isEmpty(params.search_bu)) null else "%$(params.search_bu as String)%",
	search_barcode: if (isEmpty(params.search_barcode)) null else "%$(params.search_barcode as String)%",
	name: params.name,
	code: params.code,
	keyword: if (isEmpty(params.keyword)) null else "%$(params.keyword as String)%",
	barcode: params.barcode,
	status: params.status,

	cat_codes: cat_codes map ("lower(PH2) LIKE lower('%$(sqlEscape($))%')") joinBy " OR ",
	ids: ids map ("'$(sqlEscape($))'") joinBy ",",
	brand_names: brand_names map ("lower('$(sqlEscape($))')") joinBy ",",
	source_channels: paramSourceChannel(params.*source_channels),
	filters: params.*filters joinBy ',' default null,
	order_by: order_by,
	offset: (if (isEmpty(params.offset)) offset else params.offset default offset) as Number,
	limit: (if (isEmpty(params.limit) or params.limit <= 0) limit else params.limit default limit) as Number,
	damCatCodeIsnull: params.*has_null contains "cat_codes" default false,
	damBrandNameIsnull: params.*has_null contains "brand_names" default false,
}
