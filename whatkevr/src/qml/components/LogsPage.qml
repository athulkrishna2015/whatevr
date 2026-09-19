pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Daemon logs page: subscribes the `daemon.logs` view for the lifetime of the
// page. Rows carry time, level, and text; error/warn levels get a tinted bg.
Kirigami.ScrollablePage {
    id: root

    title: Whatevr.I18n.i18nc("@title", "Logs")
    Kirigami.Theme.colorSet: Kirigami.Theme.View

    Component.onCompleted: Whatevr.ProtocolController.openLogs()
    Component.onDestruction: Whatevr.ProtocolController.closeLogs()

    actions: [
        Kirigami.Action {
            icon.name: "edit-copy-symbolic"
            text: Whatevr.I18n.i18nc("@action:button copy all visible log lines", "Copy all")
            onTriggered: {
                const lines = []
                const count = logsList.count
                for (let i = 0; i < count; ++i) {
                    const entry = logsList.model.itemById(logsList.model.idAt(i))
                    if (entry) {
                        lines.push(((entry.time || "") + " " + (entry.level || "") + " " + (entry.text || "")).trim())
                    }
                }
                if (lines.length > 0) {
                    Whatevr.ProtocolController.copyToClipboard(lines.join("\n"))
                }
            }
        },
        Kirigami.Action {
            icon.name: "folder-open-symbolic"
            text: Whatevr.I18n.i18nc("@action:button open the daemon log folder", "Open log folder")
            onTriggered: Whatevr.ProtocolController.openLogDirectory()
        }
    ]

    ListView {
        id: logsList

        model: Whatevr.ProtocolController.logsModel
        currentIndex: -1
        reuseItems: true
        Component.onDestruction: logsList.model = null

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: !Whatevr.ProtocolController.logsLoading && logsList.count === 0
            icon.name: "document-properties-symbolic"
            text: Whatevr.ProtocolController.logsErrorText.length > 0
                  ? Whatevr.I18n.i18nc("@info placeholder for the logs list", "Could not load logs")
                  : Whatevr.I18n.i18nc("@info placeholder for the logs list", "No log entries")
            explanation: Whatevr.ProtocolController.logsErrorText.length > 0
                         ? Whatevr.ProtocolController.logsErrorText
                         : Whatevr.I18n.i18nc("@info:placeholder", "Daemon log entries will appear here when available.")
        }

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.logsLoading
            visible: running
        }

        delegate: QQC2.ItemDelegate {
            id: logDelegate

            required property var item

            width: ListView.view.width
            hoverEnabled: false

            // Long-press copies the row (time + level + text).
            onPressAndHold: {
                const parts = []
                if (logDelegate.item) {
                    if (logDelegate.item.time) {
                        parts.push(logDelegate.item.time)
                    }
                    if (logDelegate.level) {
                        parts.push(logDelegate.level.toUpperCase())
                    }
                    if (logDelegate.item.text) {
                        parts.push(logDelegate.item.text)
                    }
                }
                if (parts.length > 0) {
                    Whatevr.ProtocolController.copyToClipboard(parts.join(" "))
                }
            }

            readonly property string level: (item && item.level) ? item.level : ""
            readonly property bool isError: level === "error" || level === "fatal" || level === "panic"
            readonly property bool isWarn: level === "warn" || level === "warning"

            background: Rectangle {
                color: logDelegate.isError
                       ? Qt.alpha(Kirigami.Theme.negativeTextColor, 0.08)
                       : logDelegate.isWarn
                         ? Qt.alpha(Kirigami.Theme.neutralTextColor, 0.08)
                         : "transparent"
            }

            contentItem: RowLayout {
                spacing: Kirigami.Units.largeSpacing

                QQC2.Label {
                    Layout.preferredWidth: Kirigami.Units.gridUnit * 8
                    Layout.alignment: Qt.AlignTop
                    text: (logDelegate.item && logDelegate.item.time) ? logDelegate.item.time : ""
                    font.family: "monospace"
                    font.pixelSize: Kirigami.Theme.smallFont.pixelSize
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                }

                QQC2.Label {
                    Layout.preferredWidth: Kirigami.Units.gridUnit * 3
                    Layout.alignment: Qt.AlignTop
                    text: logDelegate.level.toUpperCase()
                    font.family: "monospace"
                    font.pixelSize: Kirigami.Theme.smallFont.pixelSize
                    font.weight: Font.DemiBold
                    color: logDelegate.isError
                           ? Kirigami.Theme.negativeTextColor
                           : logDelegate.isWarn
                             ? Kirigami.Theme.neutralTextColor
                             : Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                }

                TextEdit {
                    Layout.fillWidth: true
                    Layout.alignment: Qt.AlignTop
                    text: (logDelegate.item && logDelegate.item.text) ? logDelegate.item.text : ""
                    font.family: "monospace"
                    font.pixelSize: Kirigami.Theme.smallFont.pixelSize
                    color: Kirigami.Theme.textColor
                    wrapMode: TextEdit.Wrap
                    readOnly: true
                    selectByMouse: true
                    selectByKeyboard: true
                    persistentSelection: true
                }
            }
        }
    }
}
