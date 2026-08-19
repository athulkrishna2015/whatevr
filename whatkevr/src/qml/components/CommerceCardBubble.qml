// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A product, an order or a payment.
 *
 * Three wire shapes, one card, because they are one card: a picture, a name, a
 * sum of money and a line saying where it stands. What differs between them is
 * which of those is missing and what the status line says, not the shape.
 *
 * The money arrives as an integer and a currency code rather than as text, so
 * it is formatted here, against this machine's locale. A daemon that formatted
 * it would be guessing at a reader it has never met, and a price rendered in
 * the wrong convention is a price somebody misreads.
 */
Item {
    id: root

    objectName: "commerceCardBubble"

    required property ChatBubble row

    readonly property var card: row.commerce ?? ({})

    readonly property string commerceKind: String(card.kind ?? "")
    readonly property string title: String(card.title ?? "")
    readonly property string description: String(card.description ?? "")
    readonly property string bodyText: String(card.body ?? "")
    readonly property string footerText: String(card.footer ?? "")
    readonly property string note: String(card.note ?? "")
    readonly property string thumbnailPath: String(card.thumbnail_path ?? "")
    readonly property string url: String(card.url ?? "")
    readonly property string currency: String(card.currency ?? "")
    readonly property real amount1000: card.amount_1000 ?? 0
    readonly property real salePrice1000: card.sale_price_1000 ?? 0
    readonly property int itemCount: card.item_count ?? 0
    readonly property string status: String(card.status ?? "")
    readonly property string service: String(card.service ?? "")
    readonly property int expiresAt: card.expires_at ?? 0

    readonly property bool isPayment: commerceKind.startsWith("payment")
    readonly property bool hasPicture: thumbnailPath.length > 0
    /// A discounted price. Both numbers are shown, because the saving is the
    /// claim being made and one number on its own does not make it.
    readonly property bool onSale: salePrice1000 > 0 && salePrice1000 < amount1000

    readonly property real contentMargin: Kirigami.Units.smallSpacing
    readonly property real cornerRadius: Kirigami.Units.cornerRadius
    readonly property real thumbSize: Kirigami.Units.gridUnit * 3.4

    /// The compact card asks for the width of its own words. A payment is two
    /// short lines and a sum, and stretched to the ceiling it is a bubble
    /// mostly full of nothing.
    ///
    /// The thumbnail is counted whether or not there is a picture, because the
    /// square is drawn either way: a card with no picture keeps it as a glyph
    /// so a run of these shares one left edge. Leaving it out of the width was
    /// a card a thumbnail too narrow, with its name wrapped and its status
    /// elided to make room for a square that was there regardless.
    readonly property real preferredWidth: {
        let text = Math.max(titleMetrics.advanceWidth, priceMetrics.advanceWidth)
        text = Math.max(text, statusMetrics.advanceWidth)
        return contentMargin * 2 + thumbSize + Kirigami.Units.smallSpacing
            + Math.max(text, Kirigami.Units.gridUnit * 10)
    }

    implicitWidth: row.attachmentBlockWidth
    // See InteractiveBubble: a layout has no implicit height until it first
    // arranges, and a row that reserves nothing and then grows is a row that
    // shoves the transcript under it.
    readonly property real unmeasuredBodyHeight: Kirigami.Units.gridUnit * 4
    implicitHeight: contentMargin * 2
                    + (body.implicitHeight > 0 ? body.implicitHeight : unmeasuredBodyHeight)

    /// A symbol for the codes people actually see, and the code itself for the
    /// rest. "INR 449.00" is correct and readable; a wrong symbol is neither.
    function currencySymbol(code) {
        switch (code) {
        case "INR":
            return "₹"
        case "USD":
            return "$"
        case "EUR":
            return "€"
        case "GBP":
            return "£"
        case "JPY":
            return "¥"
        case "BRL":
            return "R$"
        }
        return code.length > 0 ? code + " " : ""
    }

    /// Thousandths of a unit, which is how WhatsApp counts money everywhere in
    /// this family. Whole amounts drop their decimals: "₹449" reads as a price
    /// and "₹449.00" reads as an invoice.
    function formatMoney(thousandths) {
        if (thousandths <= 0) {
            return ""
        }
        const value = thousandths / 1000
        const decimals = Math.abs(value - Math.round(value)) < 0.005 ? 0 : 2
        return root.currencySymbol(root.currency) + Number(value).toLocaleString(Qt.locale(), 'f', decimals)
    }

    readonly property string priceText: formatMoney(onSale ? salePrice1000 : amount1000)
    readonly property string strikeText: onSale ? formatMoney(amount1000) : ""

    /// The line under the name: what this card is and where it stands. It is
    /// the difference between a shop window and a receipt.
    readonly property string statusText: {
        switch (root.commerceKind) {
        case "order":
            switch (root.status) {
            case "accepted":
                return Whatevr.I18n.i18nc("@label order status", "Accepted")
            case "declined":
                return Whatevr.I18n.i18nc("@label order status", "Declined")
            case "inquiry":
                return Whatevr.I18n.i18nc("@label order status", "Awaiting a reply")
            }
            if (root.itemCount > 0) {
                return Whatevr.I18n.i18ncp("@label number of items in an order",
                                           "%1 item", "%1 items", root.itemCount)
            }
            return Whatevr.I18n.i18nc("@label", "Order")
        case "payment_request":
            return Whatevr.I18n.i18nc("@label payment status", "Payment requested")
        case "payment_sent":
            return Whatevr.I18n.i18nc("@label payment status", "Payment sent")
        case "payment_invite":
            return root.service.length > 0
                ? Whatevr.I18n.i18nc("@label payment invitation naming the rail, %1 is e.g. UPI",
                                     "Invitation to pay over %1", root.service.toUpperCase())
                : Whatevr.I18n.i18nc("@label payment invitation", "Invitation to set up payments")
        }
        if (root.itemCount > 0) {
            return Whatevr.I18n.i18ncp("@label number of items", "%1 item", "%1 items", root.itemCount)
        }
        return ""
    }

    readonly property string headline: {
        if (root.title.length > 0) {
            return root.title
        }
        if (root.note.length > 0) {
            return root.note
        }
        return root.statusText
    }

    readonly property string kindGlyph: {
        switch (root.commerceKind) {
        case "order":
            return "view-list-text-symbolic"
        case "payment_request":
        case "payment_sent":
        case "payment_invite":
            return "wallet-open-symbolic"
        }
        return "package-symbolic"
    }

    TextMetrics {
        id: titleMetrics

        font: titleLabel.font
        text: root.headline
    }

    TextMetrics {
        id: priceMetrics

        font: priceLabel.font
        text: root.priceText
    }

    TextMetrics {
        id: statusMetrics

        font: statusLabel.font
        text: root.statusText
    }

    Rectangle {
        anchors.fill: parent
        radius: root.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    ColumnLayout {
        id: body

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            // The picture, or the glyph that stands in for it. A card with
            // neither steps in by a thumbnail's width and breaks the left edge
            // a column of these otherwise shares.
            Rectangle {
                Layout.alignment: Qt.AlignTop
                implicitWidth: root.thumbSize
                implicitHeight: root.thumbSize
                radius: Math.round(root.cornerRadius * 0.75)
                color: Qt.alpha(Kirigami.Theme.textColor, 0.06)

                Kirigami.Icon {
                    anchors.centerIn: parent
                    visible: !root.hasPicture
                    implicitWidth: Kirigami.Units.iconSizes.medium
                    implicitHeight: implicitWidth
                    source: root.kindGlyph
                    color: Kirigami.Theme.disabledTextColor
                    isMask: true
                }

                Image {
                    id: thumbSource

                    visible: false
                    anchors.fill: parent
                    source: root.hasPicture
                        ? Whatevr.ProtocolController.localFileUrl(root.thumbnailPath)
                        : ""
                    fillMode: Image.PreserveAspectCrop
                    asynchronous: true
                    cache: true
                    // No sourceSize: this is the small JPEG that arrived
                    // inside the message, and pinning the decode to the slot's
                    // width would upscale a hundred-pixel picture into a
                    // megapixel one, again on every relayout. Decoding it at
                    // its own size and letting the scene graph scale it up is
                    // both cheaper and no blurrier.
                }

                RoundedImage {
                    anchors.fill: parent
                    visible: thumbSource.status === Image.Ready
                    source: thumbSource
                    topLeftRadius: parent.radius
                    topRightRadius: parent.radius
                    bottomLeftRadius: parent.radius
                    bottomRightRadius: parent.radius
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignTop
                spacing: Kirigami.Units.smallSpacing / 2

                Controls.Label {
                    id: titleLabel

                    Layout.fillWidth: true
                    visible: root.headline.length > 0
                    text: root.headline
                    wrapMode: Text.Wrap
                    elide: Text.ElideRight
                    maximumLineCount: 2
                    font.pointSize: root.row.bodyPointSize
                    font.bold: true
                }

                // The money, in tabular figures. A column of prices that do not
                // line up is a column somebody has to read twice.
                RowLayout {
                    Layout.fillWidth: true
                    visible: root.priceText.length > 0
                    spacing: Kirigami.Units.smallSpacing

                    Controls.Label {
                        id: priceLabel

                        text: root.priceText
                        font.pointSize: root.row.bodyPointSize
                        font.bold: true
                        font.features: ({ "tnum": 1 })
                    }

                    Controls.Label {
                        visible: root.strikeText.length > 0
                        text: root.strikeText
                        color: Kirigami.Theme.disabledTextColor
                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                        font.strikeout: true
                        font.features: ({ "tnum": 1 })
                    }

                    Item { Layout.fillWidth: true }
                }

                Controls.Label {
                    id: statusLabel

                    Layout.fillWidth: true
                    visible: root.statusText.length > 0 && root.statusText !== root.headline
                    text: root.statusText
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.description.length > 0
            text: root.description
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 3
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.bodyText.length > 0
            text: root.bodyText
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.footerText.length > 0
            text: root.footerText
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // The only thing on this card the desktop can carry out. A product's
        // page is a link; paying is not, and there is no button here that
        // pretends otherwise.
        CardActionButton {
            Layout.alignment: Qt.AlignLeft
            visible: root.url.length > 0
            enabled: !root.row.selectionModeActive
            text: Whatevr.I18n.i18nc("@action open a product's page in a browser", "Open in browser")
            iconName: "link-symbolic"
            onClicked: Qt.openUrlExternally(root.url)
        }

        // Paying happens on the phone, and this says so once rather than
        // offering a button that would have to explain itself after the fact.
        Controls.Label {
            Layout.fillWidth: true
            visible: root.isPayment
            text: Whatevr.I18n.i18nc("@info payments are not handled on the desktop",
                                     "Payments are handled by WhatsApp on your phone")
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }
}
