pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Shows what sits behind a group invite link — name, description, member
// count, permission flags — before the user commits to joining. The daemon
// resolves the link through GetGroupInfoFromLink; joining uses the same
// one-shot as the invite bubble, just started from a link.
CenteredDialog {
    id: root

    readonly property var preview: Whatevr.ProtocolController.groupInvitePreview ?? ({})

    title: String(root.preview.name ?? "")
    preferredWidth: Kirigami.Units.gridUnit * 24

    function openPreview() {
        open()
    }

    onClosed: Whatevr.ProtocolController.clearGroupInvitePreview()

    QQC2.Label {
        visible: String(root.preview.topic ?? "").length > 0
        text: String(root.preview.topic ?? "")
        textFormat: Text.PlainText
        wrapMode: Text.Wrap
        Layout.fillWidth: true
    }

    QQC2.Label {
        text: Whatevr.I18n.i18ncp("@label group preview member count",
                                   "%1 member",
                                   "%1 members",
                                   Number(root.preview.member_count ?? 0))
        textFormat: Text.PlainText
        Layout.fillWidth: true
    }

    QQC2.Label {
        visible: Boolean(root.preview.announce ?? false)
        text: Whatevr.I18n.i18nc("@label group preview announce flag",
                                  "Only admins can send messages")
        textFormat: Text.PlainText
        Layout.fillWidth: true
    }

    QQC2.Label {
        visible: Boolean(root.preview.locked ?? false)
        text: Whatevr.I18n.i18nc("@label group preview locked flag",
                                  "Only admins can edit group info")
        textFormat: Text.PlainText
        Layout.fillWidth: true
    }

    QQC2.Label {
        visible: Boolean(root.preview.approval ?? false)
        text: Whatevr.I18n.i18nc("@label group preview approval flag",
                                  "An admin must approve you before you join")
        textFormat: Text.PlainText
        Layout.fillWidth: true
    }

    customFooterActions: [
        Kirigami.Action {
            text: Whatevr.I18n.i18nc("@action:button dismiss group invite preview",
                                     "Cancel")
            icon.name: "dialog-cancel-symbolic"
            onTriggered: root.close()
        },
        Kirigami.Action {
            text: Whatevr.I18n.i18nc("@action:button join previewed group", "Join")
            icon.name: "list-add-symbolic"
            onTriggered: {
                Whatevr.ProtocolController.joinPreviewedGroup()
                root.close()
            }
        }
    ]
}
