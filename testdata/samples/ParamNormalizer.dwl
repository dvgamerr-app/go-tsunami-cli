%dw 2.0
var enum_channel = {
	"home_online": 'HOFM,HOLN,HODP',
	"new_contact_center": "CC",
	"home_calling": "CCHC",
	"authorized_dealer": "AD",
	"home_solution": "HS",
	"q_chang": "QC",
	"boonthavorn": "BV",
	"dap": "HODP",
	"dam": "DAM,DM",
	"xp": "XP",
}

fun paramOrderBy(enum_items: Object, default_item: String, primary_item: String, order_items) = ((order_items default (default_item splitBy ',')) + primary_item) map ((item, index) -> do {
	var column = lower(item) splitBy ":"
	var verifyOrderBy = sizeOf(column) > 1 and (column[1] == 'asc' or column[1] == 'desc')
	---
	if (sizeOf(column) > 1 and verifyOrderBy) (enum_items[column[0]] splitBy ",") map ($ ++ ":$(column[1])")  joinBy ", " else null
}) filter(!isEmpty($)) joinBy ','


fun paramSourceChannel(source_items) = source_items map(enum_channel[lower($)]) filter(!isEmpty($)) joinBy "," splitBy "," map ("'$(sqlEscape($))'") joinBy ","

fun sqlEscape(text) = text replace "'" with("''")