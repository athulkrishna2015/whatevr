pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import Qt.labs.platform as Platform
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Posts a text or photo status of your own. Text posts immediately; photos go
// through the file picker and post with the caption line.
QQC2.Dialog {
    id: root

    title: Whatevr.I18n.i18nc("@title:window post a status", "New status")
    modal: true
    standardButtons: QQC2.Dialog.Close

    ColumnLayout {
        width: parent.width
        spacing: Kirigami.Units.largeSpacing

        QQC2.TextField {
            id: statusText

            Layout.fillWidth: true
            placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "Type a status…")
        }

        QQC2.Button {
            Layout.fillWidth: true
            text: Whatevr.I18n.i18nc("@action:button post a text status", "Post text status")
            icon.name: "document-send-symbolic"
            enabled: statusText.text.trim().length > 0
            onClicked: {
                Whatevr.ProtocolController.postStatusText(statusText.text)
                statusText.clear()
                root.close()
            }
        }

        QQC2.Button {
            Layout.fillWidth: true
            text: Whatevr.I18n.i18nc("@action:button post a photo status", "Post a photo…")
            icon.name: "camera-photo-symbolic"
            onClicked: photoDialog.open()
        }
    }

    Platform.FileDialog {
        id: photoDialog

        title: Whatevr.I18n.i18nc("@title:window", "Post a photo status")
        fileMode: Platform.FileDialog.OpenFile
        nameFilters: [Whatevr.I18n.i18nc("@item:inlistbox", "Images (*.png *.jpg *.jpeg *.webp)")]
        onAccepted: {
            Whatevr.ProtocolController.postStatusMedia(file, statusText.text)
            statusText.clear()
            root.close()
        }
    }
}
