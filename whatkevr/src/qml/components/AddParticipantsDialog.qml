pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Adds contacts to an open group. Same picker as the new-group dialog, minus
// the name field; contacts already in the group are passed in as `excluded`.
CenteredDialog {
    id: root

    readonly property real pickerWidth: Kirigami.Units.gridUnit * 22

    property string chatId: ""

    title: Whatevr.I18n.i18nc("@title:dialog", "Add participants")
    standardButtons: Kirigami.Dialog.Cancel
    preferredWidth: root.pickerWidth

    Component.onCompleted: contentItem.ScrollBar.vertical.policy = ScrollBar.AlwaysOff

    // excluded: a jid-keyed set of the members the group already has.
    function openFor(groupId, excluded) {
        chatId = groupId
        picker.reset()
        picker.excluded = excluded || ({})
        Whatevr.ProtocolController.openContactTargets()
        open()
    }

    onClosed: {
        Whatevr.ProtocolController.closeContactTargets()
        picker.reset()
        picker.excluded = ({})
        chatId = ""
    }

    customFooterActions: [
        Kirigami.Action {
            text: Whatevr.I18n.i18nc("@action:button add the selected participants", "Add")
            icon.name: "list-add-user-symbolic"
            enabled: root.chatId.length > 0 && picker.selectedCount > 0
            onTriggered: {
                Whatevr.ProtocolController.updateGroupMembers(root.chatId, "add", picker.selectedJids())
                root.close()
            }
        }
    ]

    ColumnLayout {
        implicitWidth: root.pickerWidth
        spacing: Kirigami.Units.smallSpacing

        ContactMultiPicker {
            id: picker

            Layout.fillWidth: true
        }
    }
}
