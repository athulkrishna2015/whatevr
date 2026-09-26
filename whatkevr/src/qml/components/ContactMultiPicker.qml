pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr
import "Initials.js" as Initials

// Multi-select contact list shared by the new-group and add-participants
// dialogs. Rows are the daemon's direct chats (the synced contact list) in
// daemon order; the search box narrows the rows the picker already holds and
// `excluded` keeps contacts the caller already has out of the offering — both
// presentation-side, over rows the frontend already has. Selection is owned by
// the picker; the dialogs read it back through selectedJids().
ColumnLayout {
    id: root

    // Fixed content width used for both the implicit width and the ListView's,
    // so a picker never feeds its width back into the dialog's centred geometry
    // (same note as ForwardChatPickerDialog).
    readonly property real pickerWidth: Kirigami.Units.gridUnit * 22
    // Tallest the contact list grows before it scrolls internally.
    readonly property real listMax: Kirigami.Units.gridUnit * 14

    // jid -> { name, avatarLocalPath, initials }.
    property var selected: ({})
    property int selectedCount: 0
    // Contacts the caller already holds, keyed by jid.
    property var excluded: ({})

    // The revision tick is what makes this re-evaluate when the view changes:
    // a Q_INVOKABLE call on its own gives the binding no property to track.
    readonly property var targets: {
        if (Whatevr.ProtocolController.contactTargetsRevision < 0) {
            return []
        }
        const rows = Whatevr.ProtocolController.contactTargets(searchField.text)
        const out = []
        for (let i = 0; i < rows.length; ++i) {
            const jid = String(rows[i].id || "")
            if (root.excluded[jid] === undefined) {
                out.push(rows[i])
            }
        }
        return out
    }

    implicitWidth: pickerWidth
    spacing: Kirigami.Units.smallSpacing

    function reset() {
        selected = ({})
        selectedCount = 0
        searchField.text = ""
    }

    function selectedJids() {
        return Object.keys(root.selected)
    }

    function contactInfo(chat) {
        const name = String(chat.name || "")
        return {
            name: name,
            avatarLocalPath: String(chat.avatar_path || ""),
            initials: Initials.firstLast(name)
        }
    }

    function toggle(jid, info) {
        if (jid.length === 0) {
            return
        }
        const next = Object.assign({}, root.selected)
        if (next[jid] !== undefined) {
            delete next[jid]
        } else {
            next[jid] = info || { name: "", avatarLocalPath: "", initials: "?" }
        }
        root.selected = next
        root.selectedCount = Object.keys(next).length
    }

    Kirigami.SearchField {
        id: searchField

        Layout.fillWidth: true
        placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "Search contacts…")
        Keys.onEscapePressed: text = ""
    }

    Label {
        Layout.fillWidth: true
        visible: root.selectedCount > 0
        text: Whatevr.I18n.i18nc("@info:status number of contacts selected", "%1 selected", root.selectedCount)
        color: Kirigami.Theme.disabledTextColor
        font: Kirigami.Theme.smallFont
        elide: Text.ElideRight
    }

    Item {
        id: contactListViewport

        Layout.fillWidth: true
        Layout.preferredHeight: contactList.Layout.preferredHeight

        ListView {
            id: contactList

            anchors.fill: parent
            Layout.preferredHeight: Math.min(contentHeight, root.listMax)
            clip: true
            model: root.targets
            currentIndex: -1
            boundsBehavior: Flickable.StopAtBounds
            flickableDirection: Flickable.VerticalFlick
            acceptedButtons: Qt.NoButton
            reuseItems: true
            spacing: 0
            ScrollBar.vertical: DiscreetScrollBar {}

            Label {
                anchors.centerIn: parent
                width: parent.width - Kirigami.Units.largeSpacing * 2
                visible: root.targets.length === 0
                horizontalAlignment: Text.AlignHCenter
                wrapMode: Text.WordWrap
                color: Kirigami.Theme.disabledTextColor
                text: searchField.text.length > 0
                      ? Whatevr.I18n.i18nc("@info:placeholder no search results", "No contacts found")
                      : Whatevr.I18n.i18nc("@info:placeholder no contacts", "No contacts available")
            }

            delegate: ItemDelegate {
                id: contactDelegate

                required property var modelData

                readonly property string jid: String(modelData.id || "")
                readonly property bool selected: root.selected[jid] !== undefined

                width: ListView.view.width
                highlighted: contactDelegate.selected
                onClicked: root.toggle(contactDelegate.jid, root.contactInfo(contactDelegate.modelData))

                contentItem: RowLayout {
                    spacing: Kirigami.Units.smallSpacing

                    CheckBox {
                        checked: contactDelegate.selected
                        onToggled: root.toggle(contactDelegate.jid, root.contactInfo(contactDelegate.modelData))
                    }

                    AvatarImage {
                        Layout.preferredWidth: Kirigami.Units.gridUnit * 1.65
                        Layout.preferredHeight: Kirigami.Units.gridUnit * 1.65
                        avatarLocalPath: String(contactDelegate.modelData.avatar_path || "")
                        initials: Initials.firstLast(String(contactDelegate.modelData.name || ""))
                    }

                    Label {
                        text: String(contactDelegate.modelData.name || "")
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }
            }
        }
    }
}
