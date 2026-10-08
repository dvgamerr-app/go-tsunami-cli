%dw 2.0
import * from dw::util::Values

output application/json
var orderLMSEmpty = {
  truckNo: null,
  carrierName: null,
  boxspeed: null,
  boxposition: null,
  boxGpsTime: null,
  shipment: null,
  dnNo: null,
  originname: null,
  destinationName: null,
  accDistance: null,
  goodIssueTime: null,
  plandelivery: null,
  destinationInbound: null,
  eta: null,
  etadistance: null,
  etaduration: null,
  estimatedtime: null,
  status: null,
  statusId: null,
  message: null,
  soldTo: null,
  shippingPoint: null,
  returnFlag: null,
  phoneNo: null,
  contractId: null,
  contractABBR: null,
}

var dataItemKeys=["delivery","deliveryItem", "deliveryQty","saleUnit","matDes","soNo","itemNo","matGroup1","poNo","matNumber","netWeightTon","grossWeightTon","weightUnitTon"]
var lmsKeys = ["soldTo", "destinationName"]
var lmsSapKeyMap = {
    "soldTo" : "soldTo",
    "destinationName": "shipTo"
}

var lmsItemKeys = ["truckNo", "plandelivery","shipment"]
var lmsSapItemKeyMap = {
    "truckNo" : "truckNo",
    "plandelivery": "dueDate",
    "shipment":"shipment"
    }

var partnerItem = vars.itemData.partnerData.data
var addressItem = vars.itemData.addressList.address

var orderTrackingOutput = if (!isEmpty(vars.itemData)) vars.itemData.delHeaderData.data map (head) -> do {
	var dataItems = if (!isEmpty(vars.itemData.delItemData.itemData)) (vars.itemData.delItemData.itemData filter ($.delivery == head.dpNo) ) else []
    var sapItems = if (!isEmpty(payload.orderTrackingOutput)) payload.orderTrackingOutput filter ($.dnNo == head.dpNo) else null
    var sapItem = (sapItems[0] default orderLMSEmpty)

    // Takes first element to take truckNo, dueDate and shipment
	var dataItem = dataItems[0]

    ---
    {
        dnNo: head.dpNo,
        piNumber: head.piNumber,
        (sapItem - "dnNo"),
	    cutOffDate: head.cutOffDate ,
	    cutOffTime: head.cutOffTime,
	    giTime: head.giTime,
        dataItem: (dataItems map ((dataItem, index) -> dataItem filterObject ((value, key, index) ->  (dataItemKeys contains (key) as String) ) ))
    } mapObject ((value, key, index) ->{
	//To set "soldTo", "destinationName" from sap if those are null. Assumes the key will be there always
        (key): if(isEmpty(value) and (lmsKeys contains(key as String))) head[lmsSapKeyMap[key]] else value
    } ) mapObject ((value, key, index) ->{
	//To set "truckNo", "plandelivery","shipment" from sap if those are null
        (key): if(isEmpty(value) and (lmsItemKeys contains(key as String))) dataItem[lmsSapItemKeyMap[key]] else value
    } ) mapObject ((value, key, index) ->{
        (key): if((key as String) == "destinationName") (addressItem filter ($.addNo == (partnerItem filter ($.delItem == head.dpNo and $.shipTo == head.shipTo and $.partnerFunction == "WE"))[0].addCode))[0].shipToName else value
    } )
    ++ ("truckNo":dataItem.truckNo) if(!sapItem.truckNo?) //sets truckNo if not exists from sap
    ++ ("plandelivery":dataItem.dueDate) if(!sapItem.plandelivery?)//sets plandelivery from sap if not exists
    ++ ("shipment":dataItem.shipment) if(!sapItem.shipment?)//sets shipment from sap if not exists
} else []
---
{
  'orderTrackingOutput':orderTrackingOutput,
  "message": if (payload.isError) payload.message else "success",
  "isError": payload.isError
}
