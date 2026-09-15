pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Channels tab: followed channels, with a follow action and per-row tap to
// open the channel's messages. The page owns the daemon `channels`
// subscription while on screen.
Kirigami.ScrollablePage {
    id: root

    title: Whatevr.I18n.i18nc("@title", "Channels")
    Kirigami.Theme.colorSet: Kirigami.Theme.View

    Component.onCompleted: Whatevr.ProtocolController.openChannels()
    Component.onDestruction: Whatevr.ProtocolController.closeChannels()

    actions: [
        Kirigami.Action {
            icon.name: "list-add-symbolic"
            text: Whatevr.I18n.i18nc("@action:button follow a channel", "Follow Channel")
            onTriggered: followDialog.open()
        }
    ]

    ListView {
        id: channelsList

        model: Whatevr.ProtocolController.channelsModel
        currentIndex: -1
        reuseItems: true
        Component.onDestruction: channelsList.model = null

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.channelsLoading && channelsList.count === 0
            visible: running
        }

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: channelsList.count === 0 && !Whatevr.ProtocolController.channelsLoading
            icon.name: "rss-symbolic"
            text: Whatevr.I18n.i18nc("@info placeholder for the channels list", "No channels")
            explanation: Whatevr.I18n.i18nc("@info:placeholder", "Follow a channel to see its broadcasts here.")
        }

        delegate: QQC2.ItemDelegate {
            id: channelDelegate

            required property var item

            width: ListView.view.width

            onClicked: {
                applicationWindow().pageStack.layers.push(
                    Qt.resolvedUrl("ChannelMessagesPage.qml"), {
                        "channelJid": channelDelegate.item.jid || channelDelegate.item.id || "",
                        "channelName": channelDelegate.item.name || ""
                    })
            }

            contentItem: RowLayout {
                spacing: Kirigami.Units.largeSpacing

                Kirigami.Icon {
                    Layout.preferredWidth: Kirigami.Units.gridUnit * 2
                    Layout.preferredHeight: Kirigami.Units.gridUnit * 2
                    source: "rss-symbolic"
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Kirigami.Units.smallSpacing / 2

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: channelDelegate.item.name || ""
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: channelDelegate.item.description || ""
                        color: Kirigami.Theme.disabledTextColor
                        elide: Text.ElideRight
                        maximumLineCount: 1
                    }
                }

                Kirigami.Icon {
                    Layout.preferredWidth: Kirigami.Units.iconSizes.small
                    Layout.preferredHeight: Kirigami.Units.iconSizes.small
                    source: "audio-volume-muted-symbolic"
                    visible: channelDelegate.item.muted === true
                    opacity: 0.5
                }
            }
        }
    }

    Kirigami.PromptDialog {
        id: followDialog
        title: Whatevr.I18n.i18nc("@title:dialog", "Follow Channel")
        subtitle: Whatevr.I18n.i18nc("@info", "Paste a channel invite link or JID.")

        standardButtons: Kirigami.Dialog.Ok | Kirigami.Dialog.Cancel

        QQC2.TextField {
            id: followInput
            placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "https://whatsapp.com/channel/... or JID")
        }

        onAccepted: {
            const text = followInput.text.trim()
            if (text.length > 0) {
                Whatevr.ProtocolController.followChannel(text)
            }
            followInput.clear()
        }
        onRejected: followInput.clear()
    }
}
