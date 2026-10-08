%dw 2.0
output application/json skipNullOn="everywhere"
---
{
    WEB_TRANSACTION_ID: payload.piMessageId,
    SALESDOCUMENTIN: payload.salesdocumentin,
    ORDER_HEADER_IN:  {
        DOC_TYPE: payload.orderHeaderIn.docType,
        SALES_ORG: payload.orderHeaderIn.salesOrg,
        DISTR_CHAN: payload.orderHeaderIn.distributionChannel,
        DIVISION: payload.orderHeaderIn.division,
        REQ_DATE_H: payload.orderHeaderIn.requestDate,
        PRICE_DATE: payload.orderHeaderIn.priceDate,
        CURRENCY: payload.orderHeaderIn.currency,
        TAXK1: payload.orderHeaderIn.taxClass,
    },
    ORDER_PARTNERS: payload.orderPartners map (e, i) -> {
        PARTN_ROLE: e.partnerRole,
        PARTN_NUMB: e.partnerNo,
        ITM_NUMBER: e.itemNo,
    },
    ORDER_ITEMS_IN: payload.orderItemsIn map (e, i) -> {
        ITM_NUMBER: e.itemNo,
        MATERIAL: e.material,
        CUST_MAT35: e.custMat35,
        TARGET_QTY: e.targetQuantity,
        SALES_UNIT: e.salesUnit,
        PLANT: e.plant,
        ITEM_CATEG: e.itemCategory,
        REASON_REJ: e.rejectReason,
        PRICE_DATE: e.priceDate,
        HL_ITM: e.parentItemNo,
    },
    ORDER_SCHEDULES_IN: payload.orderSchedulesIn map (e, i) -> {
        ITM_NUMBER: e.itemNo,
        SCHED_LINE: e.scheduleLine,
        SCHED_TYPE: e.scheduleLineCate,
        REQ_DATE: e.requestDate,
        REQ_QTY: e.requestQuantity,
        CONFIRM_QTY: e.confirmQtuantity,
        REQ_DLV_BL: e.deliveryBlock,
    },
}