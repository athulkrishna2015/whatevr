pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Creates a group: a name plus a multi-select over the synced contact list.
// `group.create` answers with the new chat's id and the controller takes it
// from there (select + surface), so this dialog only has to close.
CenteredDialog {
    id: root

    readonly property real pickerWidth: Kirigami.Units.gridUnit * 22

    title: Whatevr.I18n.i18nc("@title:dialog", "New group")
    standardButtons: Kirigami.Dialog.Cancel
    preferredWidth: root.pickerWidth

    // Kirigami.Dialog wraps content in a QQC2.ScrollView with its own scrollbar;
    // the contact list already scrolls, so suppress the dialog's bar.
    Component.onCompleted: contentItem.ScrollBar.vertical.policy = ScrollBar.AlwaysOff

    function openFor() {
        nameField.text = ""
        picker.reset()
        // The picker's own `chats` subscription lives exactly as long as the
        // dialog does.
        Whatevr.ProtocolController.openContactTargets()
        open()
    }

    onClosed: {
        Whatevr.ProtocolController.closeContactTargets()
        picker.reset()
    }

    customFooterActions: [
        Kirigami.Action {
            text: Whatevr.I18n.i18nc("@action:button create the group", "Create")
            icon.name: "list-add-symbolic"
            enabled: nameField.text.trim().length > 0
            onTriggered: {
                Whatevr.ProtocolController.createGroup(nameField.text.trim(), picker.selectedJids())
                root.close()
            }
        }
    ]

    ColumnLayout {
        // Pin the content width like the other in-app dialogs; Kirigami.Dialog
        // does not give a bare content item a width on its own.
        implicitWidth: root.pickerWidth
        spacing: Kirigami.Units.smallSpacing

        QQC2.TextField {
            id: nameField

            Layout.fillWidth: true
            placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "Group name")
            onAccepted: if (nameField.text.trim().length > 0) {
                Whatevr.ProtocolController.createGroup(nameField.text.trim(), picker.selectedJids())
                root.close()
            }
        }

        ContactMultiPicker {
            id: picker

            Layout.fillWidth: true
        }
    }
}
