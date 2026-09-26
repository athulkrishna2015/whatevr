pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr
import "Initials.js" as Initials

// Picks one of the account's groups to attach under a community (admins). Rows
// come from the daemon's `chats` view through the same subscription the forward
// picker opens — both need every chat in the account — narrowed here to groups
// and minus the ones the community already links. That narrowing is
// presentation-side over rows the frontend already holds, not a second view
// (PROTOCOL.md on member search; the same rule applies to any picker).
CenteredDialog {
    id: root

    readonly property real pickerWidth: Kirigami.Units.gridUnit * 22
    // Tallest the group list grows before it scrolls internally, so the
    // dialog's own ScrollView never has to.
    readonly property real listMax: Kirigami.Units.gridUnit * 14

    property string communityId: ""
    // group id -> true, the groups this community already links (plus the
    // community itself, which cannot link under itself).
    property var excludedGroups: ({})

    // The one row picked so far; single-select because linking is a one-shot
    // admin action rather than a batch.
    property string selectedId: ""

    title: Whatevr.I18n.i18nc("@title:dialog", "Link group")
    standardButtons: Kirigami.Dialog.Cancel
    preferredWidth: root.pickerWidth

    Component.onCompleted: contentItem.ScrollBar.vertical.policy = ScrollBar.AlwaysOff

    function openFor(communityId, excluded) {
        root.communityId = communityId || ""
        root.selectedId = ""
        root.excludedGroups = excluded || ({})
        searchField.text = ""
        // The picker's `chats` subscription lives exactly as long as the
        // dialog does; the daemon owns the rows and their order.
        Whatevr.ProtocolController.openForwardTargets()
        open()
    }

    onClosed: {
        Whatevr.ProtocolController.closeForwardTargets()
        root.excludedGroups = ({})
        root.selectedId = ""
        root.communityId = ""
    }

    // The revision tick is what makes this re-evaluate when the view changes:
    // a Q_INVOKABLE call on its own gives the binding no property to track.
    readonly property var candidates: {
        if (Whatevr.ProtocolController.forwardTargetsRevision < 0) {
            return []
        }
        const rows = Whatevr.ProtocolController.forwardChatTargets(searchField.text)
        const out = []
        for (let i = 0; i < rows.length; ++i) {
            const id = String(rows[i].id || "")
            if (!rows[i].is_group || id === root.communityId
                || root.excludedGroups[id] !== undefined) {
                continue
            }
            out.push(rows[i])
        }
        return out
    }

    customFooterActions: [
        Kirigami.Action {
            text: Whatevr.I18n.i18nc("@action:button link the selected group", "Link")
            icon.name: "list-add-user-symbolic"
            enabled: root.communityId.length > 0 && root.selectedId.length > 0
            onTriggered: {
                Whatevr.ProtocolController.linkCommunityGroup(root.communityId, root.selectedId)
                root.close()
            }
        }
    ]

    ColumnLayout {
        implicitWidth: root.pickerWidth
        spacing: Kirigami.Units.smallSpacing

        Kirigami.SearchField {
            id: searchField

            Layout.fillWidth: true
            placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "Search groups…")
            Keys.onEscapePressed: text = ""
        }

        Item {
            id: groupListViewport

            Layout.fillWidth: true
            Layout.preferredHeight: groupList.Layout.preferredHeight

            ListView {
                id: groupList

                anchors.fill: parent
                Layout.preferredHeight: Math.min(contentHeight, root.listMax)
                clip: true
                model: root.candidates
                currentIndex: -1
                boundsBehavior: Flickable.StopAtBounds
                flickableDirection: Flickable.VerticalFlick
                acceptedButtons: Qt.NoButton
                reuseItems: true
                spacing: 0
                ScrollBar.vertical: DiscreetScrollBar {}

                Kirigami.PlaceholderMessage {
                    anchors.centerIn: parent
                    width: parent.width - Kirigami.Units.gridUnit * 2
                    visible: groupList.count === 0
                    icon.name: "group-symbolic"
                    text: searchField.text.length > 0
                          ? Whatevr.I18n.i18nc("@info:placeholder no search results", "No groups found")
                          : Whatevr.I18n.i18nc("@info:placeholder no groups", "No other groups to link")
                }

                delegate: ItemDelegate {
                    id: groupDelegate

                    required property var modelData

                    readonly property string groupId: String(modelData.id || "")

                    width: ListView.view.width
                    highlighted: groupDelegate.groupId === root.selectedId
                    onClicked: root.selectedId = groupDelegate.groupId

                    contentItem: RowLayout {
                        spacing: Kirigami.Units.smallSpacing

                        AvatarImage {
                            Layout.preferredWidth: Kirigami.Units.gridUnit * 1.65
                            Layout.preferredHeight: Kirigami.Units.gridUnit * 1.65
                            avatarLocalPath: String(groupDelegate.modelData.avatar_path || "")
                            initials: Initials.firstTwo(String(groupDelegate.modelData.name || ""))
                        }

                        Label {
                            Layout.fillWidth: true
                            text: String(groupDelegate.modelData.name || "")
                            elide: Text.ElideRight
                        }

                        Kirigami.Icon {
                            visible: groupDelegate.highlighted
                            source: "checkmark-symbolic"
                            color: Kirigami.Theme.highlightColor
                            isMask: true
                        }
                    }
                }
            }
        }
    }
}
