%dw 2.0
output application/json
fun nullIfEmpty(value) = if (!isEmpty(value)) value else null

var enum_channel = {
	'HOFM': "home_online",
	'HOLN': "home_online",
	'HODP': "home_online",
	"CC": "new_contact_center",
	"CCHC": "home_calling",
	"AD": "authorized_dealer",
	"HS": "home_solution",
	"QC": "q_chang",
	"BV": "boonthavorn",
	"HODP": "dap",
	"DAM": "dam",
	"DM": "dam",
	"XP": "xp",
}
---
{
  limit: vars.query_param.limit,
  offset: vars.query_param.offset,
  total: payload[0].payload.total,
  results: payload[1].payload map ((item) -> {
    refId: item.refId,
    channel: nullIfEmpty(enum_channel[item.channel]),
    name: nullIfEmpty(item.name),
    bpId: nullIfEmpty(item.bpId),
    status: nullIfEmpty(item.status),
    address: {
      address: nullIfEmpty(item.address),
      subDistrict: nullIfEmpty(item.subDistrict),
      postCode: nullIfEmpty(item.postCode),
      district: nullIfEmpty(item.district),
      province: nullIfEmpty(item.province),
      region: nullIfEmpty(item.region),
    },
    openingHours: nullIfEmpty(item.openingHours),
    openingHoursSpecial: nullIfEmpty(item.openingHoursSpecial),
    noteStatus: null,
    secondaryStore: null,
    phones: if (isEmpty(item.phones)) [] else item.phones splitBy "|" map ({
      number: $,
      numberExt: null
    }),
    mapUrl: nullIfEmpty(item.mapUrl),
    mapFileUrl: null,
    directionTip: null,
    product: null,
    categories: [],
    contact: nullIfEmpty(item.contact),
    saleAreaCode: nullIfEmpty(item.saleAreaCode),
    saleGroupCode: nullIfEmpty(item.saleGroupCode),
    customerGroupCode: nullIfEmpty(item.customerGroupCode),
    customerGroupName: nullIfEmpty(item.customerGroupName),
    createdAt: item.createdAt,
    updatedAt: item.updatedAt,
  })
}
