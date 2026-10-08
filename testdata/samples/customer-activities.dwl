%dw 2.0
import * from dw::System
import try from dw::Runtime

output application/json

fun parseCustomerFromAttribute(header) = do {
	var customer = header.CustomerLongText splitBy "|"
	---
	{
		customerID: header.CustomerId,
		name: trim(customer[3]) default header.CustomerName,
		mobileNo: header.Tel,
		email: header.Email, // None
		address: {
			name: trim(customer[3]),
			mobileNo: header.Tel,
			address: customer[10],
			subDistrict: customer[15],
			district: customer[13],
			province: customer[11],
			zipcode: customer[14]
		},
		deliveryAddress: {
			name: trim(customer[3]),
			mobileNo: header.Tel,
			address: customer[10],
			subDistrict: customer[15],
			district: customer[13],
			province: customer[11],
			zipcode: customer[14]
		},
		customerType: header.CustomerType
	}
}

fun parseProductsFromAttribute(detail) = detail map (item, i) -> {
	id: item.ProductCode default item.ProductId,
	name: item.Name default item.ProductIdName,
	pricePerUnit: parseDecimal(item.PricePerUnit default item.UnitPrice default 0),
	quantity: parseNumber(item.Quantity default 0),
	unit: item.UnitCode default item.Unit
}

fun getTypeBuyNow(header, detail) = {
	refFMSId: [
		header.order_id
	],
	products: if (sizeOf(detail) > 0) parseProductsFromAttribute(detail) else [
		{
			id: header.first_item_sku_id,
			name: header.first_item_name,
			pricePerUnit: parseDecimal(header.first_item_price default 0),
			quantity: parseNumber(header.first_item_qty default 0),
			unit: header.first_item_unit_code
		}
	],
	customer: {
		name: header.recipients_name,
		mobileNo: header.recipients_tel,
		email: header.recipients_email,
	}
}

fun getTypeDap(header, detail) = {
	buynowStatus: header.is_buynow_payment_status,
	isBuynow: header.is_buynow == "YES",
	customer: {
		name: "$(header.contact_firstname) $(header.contact_lastname)",
		mobileNo: header.contact_phone,
		email: null
	},
	serviceProviders: [
		header.service_provider_name
	],
	serviceNames: [
		header.detail_remark
	],
	appointmentDate: if (isEmpty(header.appointment_date)) null else (header.appointment_date as Date {format: "yyyy-MM-dd"} ++ |00:00:00Z|) as String
}

fun getTypeChang(header, detail) = {
	"type": header.serviceTypeDescription,
	customer: {
		name: if (isEmpty(header.firstname) and isEmpty(header.lastname)) null else "$(header.firstname default '') $(header.lastname default '')",
		mobileNo: header.phoneNo,
		email: header.email,
		address: {
			name: if (isEmpty(header.firstname) and isEmpty(header.lastname)) null else "$(header.firstname default '') $(header.lastname default '')",
			mobileNo: header.phoneNo,
			address: header.address,
			subDistrict: header.subDistrict,
			district: header.district,
			province: header.province,
			zipcode: header.zipcode,
		}
	},
	products: if (sizeOf(detail) > 0) parseProductsFromAttribute(detail) else [
		{
			id: header.orderCode,
			name: header.title,
			pricePerUnit: parseDecimal(header.totalPrice default 0),
			quantity: parseNumber(header.qty default 0),
			unit: null
		}
	],
	paymentType: header.paymentType, // None
	paymentStatus: header.paymentStatus, // None
	discount: parseDecimal(header.discount default 0),
	totalPrice: parseDecimal(header.totalPrice default 0),
	couponCode: header.couponCode,
	paidDate: if (isEmpty(header.paidDate)) null else (header.paidDate as LocalDateTime {format: "yyyy-MM-dd'T'HH:mm:ss.SSS"}) as String ++ "Z", // None
}

fun getTypeQuotation(header, detail) = {
	products: parseProductsFromAttribute(detail),
	customer: parseCustomerFromAttribute(header),
	quotationOrder: header.QuotationOrder default header.StatusWinLoss,
	causeOfWinLoss: header.CauseOfWinLoss,
	partner: {
		code: header.SellerCode,
		name: header.SellerName,
		address: null, // None
		mobileNo: null // None
	},
	subChannel: header.Channel,
	callType: header.CallType,
	totalAmount: parseDecimal(header.TotalAmount default header.amount),
	transport: header.Transport,
	paymentType: header.PaymentType,
	district: header.District,
	province: header.Province,
	taxPayerID: header.TaxPayerID, // None
	customerPaymentDate: if (isEmpty(header.CustomerPaymentDate)) null else (header.CustomerPaymentDate as Date {format: "yyyy-MM-dd"} ++ |00:00:00Z|) as String,
	customerPayment: parseDecimal(header.CustomerPayment default 0),
}

fun getTypeSaleOrder(header, detail) = {
	p3RefID: header.SaleOrderIdOfP3,
	quotationNo: header.QuotationId,
	(getTypeQuotation(header, detail)),
}

fun parseNumber(amount) = amount as String {format: "0"} as Number
fun parseDecimal(amount) = amount as String {format: "0.00"} as Number


fun getSubjectObject(source_system, channel_type, header, detail) = upper(source_system) match {
	case "HOFM" -> getChannel(channel_type, header, detail)
	case "HOLN" -> getChannel(channel_type, header, detail)
	case "HODP" -> getChannel(channel_type, header, detail)
	case "CCHC" -> getChannel(channel_type, header, detail)
	case "BV" -> getChannelBTVSaleOrder(channel_type, header, detail)
	case "AD" -> getChannelADSaleOrder(channel_type, header, detail)
	else -> getChannelOther(channel_type, header, detail)
}

fun getChannel(channel_type, header, detail) = lower(channel_type) match {
	case "buynow" -> getTypeBuyNow(header, detail)
	case "req2buy" -> getTypeBuyNow(header, detail)
	case "dap" -> getTypeDap(header, detail)
	case "q-chang" -> getTypeChang(header, detail)
	case "quotation" -> getTypeQuotation(header, detail)
	case "sale order" -> getTypeSaleOrder(header, detail)
	else -> { error: "unknow type '$(lower(channel_type))'" }
}

fun getChannelBTVSaleOrder(channel_type, header, detail) = {
	// products: parseProductsFromAttribute(detail),
	products: if (sizeOf(detail) > 0) parseProductsFromAttribute(detail) else [
		{
			id: header.ARTICLE_ID,
			name: header.ARTICLE_NAME_TH,
			pricePerUnit: parseDecimal(header.UNIT_PRICE_INC_TAX default 0),
			quantity: parseNumber(header.SALE_QTY default 0),
			unit: header.UOM
		}
	],
	customer: {
		customerID: header.CUSTOMER_CODE,
		name: header.CUSTOMER_NAME,
		email: header.CUSTOMER_EMAIL,
		mobileNo: header.CUSTOMER_MOBILE,
		address: {
			name: header.CUSTOMER_NAME,
			mobileNo: header.CUSTOMER_MOBILE,
			address: header.CUSTOMER_ADDRESS,
			subDistrict: null, // None
			district: null, // None
			province: null, // None
			zipcode: null, // None
		}
	},
	dealer: {
		name: header.PARTNER_NAME, // None
		isrName: header.SLS_OFC_DESC, // None
		code: header.PARTNER_CODE // None
	},
	netAmount: parseDecimal(header.NET_INC_TAX default 0),
	discount: parseDecimal(header.SUP_DISC default 0 + header.VENDOR_DISC default 0 + header.WHOLESALE_DISC default 0),
}

fun getChannelADSaleOrder(channel_type, header, detail) = {
	products: [
		{
			id: header.ItemCode,
			name: header.ItemName,
			pricePerUnit: parseDecimal(header.Amount default 0),
			quantity: parseNumber(header.Qty default 0),
			unit: header.UnitCode
		}
	],
	customer: {
		customerID: header.CustCode,
		name: header.CustName,
		email: header.Email, // None
		mobileNo: header.Mobile,
		address: {
			name: header.CustName,
			mobileNo: header.Mobile,
			address: header.Customer_Address,
			subDistrict: null, // None
			district: null, // None
			province: null, // None
			zipcode: null, // None
		}
	},
	dealer: {
		name: header.DealerName,
		isrName: header.isrName, // None
		code: header.DealerCode
	},
	netAmount: parseDecimal(header.NetAmount default 0),
	discount: parseDecimal(header.DiscountAmount default 0),
}

// Type sale_order
fun getChannelOther(channel_type, header, detail) = {
	products: parseProductsFromAttribute(detail),
	customer: {
		customerID: header.CustomerId,
		name: header.CustomerIdName,
		email: header.CustomerEmail, // None
		mobileNo: header.Telephone1,
		address: {
			name: header.Name,
			mobileNo: header.Telephone1,
			address: header.scg_addressno,
			subDistrict: header.scg_subDistrictIdName, // None
			district: header.scg_districtIdName,
			province: header.scg_provinceIdName,
			zipcode: header.scg_zipcode, // None
		}
	},
	dealer: {
		name: header.scg_name, // None
		isrName: header.scg_name, // None
		code: header.scg_agentno // None
	},
	netAmount: parseDecimal(header.scg_totallineitemamount default 0),
	discount: parseDecimal(header.scg_totallineitemamount default 0 - header.scg_amountafterdiscountservice default 0),
}

var nccHeaderPhone = payload[0].payload.resultSet1
var nccDetailPhone = payload[1].payload.resultSet1
var nccHeaderTotal = payload[2].payload.resultSet1

var attrHeader = try(() -> nccHeaderPhone map ((item, i) -> {
	(item - "attribute_object"),
	attribute_object: (read(item.attribute_object, "application/json")),
}))
var attrDetail = try(() -> nccDetailPhone map ((item, i) -> {
	(item - "attribute_object"),
	attribute_object: (read(item.attribute_object, "application/json")),
}))

var isDev = envVar("env") != "prod"
var eChannel = {
	"HOFM": 'home_online',
	"HOLN": 'home_online',
	"HODP": 'home_online',
	"CC": "new_contact_center",
	"CCHC": "home_calling",
	"AD": "authorized_dealer",
	"HS": "home_solution",
	"QC": "q_chang",
	"BV": "boonthavorn",
	// "HODP": "dap",
	"DAM": "dam",
	"XP": "xp",
}
---
if (!attrHeader.success)
{
  timestamp: now() as String {format:"yyyy-MM-dd'T'HH:mm:ss'Z'"},
  status: 500,
  message: attrHeader.error.message[0 to 200],
  error: "PROC_NCC_HEADER_PHONE: $(attrHeader.error.location)",
}
else if (!attrDetail.success)
{
  timestamp: now() as String {format:"yyyy-MM-dd'T'HH:mm:ss'Z'"},
  status: 500,
  message: attrDetail.error.message[0 to 200],
  error: "PROC_NCC_DETAIL_PHONE: $(attrDetail.error.location)",
}
else
{
	limit: vars.query_param.limit,
	offset: vars.query_param.offset,
	total: nccHeaderTotal[0]['TOTAL_RECORDS'],
	result: attrHeader.result map ((item, i) -> do {
		var detail = (attrDetail.result filter (detail, i) -> (detail.activity_no == item.activity_no)) map (detail, i) -> detail.attribute_object
		---
		({
				channel: eChannel[item.source_system] default item.source_system,
				refId: item.activity_no,
				"type": item.channel_type,
				subject: getSubjectObject(item.source_system, item.channel_type, item.attribute_object, detail),
				status: item.attribute_object.status_name_th default item.activity_status,
				createDate: if (isEmpty(item.created_date)) null else (item.created_date as LocalDateTime {format: "yyyy-MM-dd'T'HH:mm:ss.SSS"}) as String ++ "Z",
				updateDate: if (isEmpty(item.updated_date)) null else (item.updated_date as LocalDateTime {format: "yyyy-MM-dd'T'HH:mm:ss.SSS"}) as String ++ "Z",
				PROC_NCC_HEADER_PHONE: item.attribute_object,
				PROC_NCC_DETAIL_PHONE: detail,
			})
			- (if (!isDev) "PROC_NCC_HEADER_PHONE" else "")
			- (if (!isDev) "PROC_NCC_DETAIL_PHONE" else "")
	}),
}
