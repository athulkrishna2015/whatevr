pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Confirm-before-send for attachments: picking files (or dropping them)
// stages them here first — thumbnails, names, a caption field and a
// Send/Cancel choice — instead of sending immediately. Nothing leaves the
// device until Send is pressed.
CenteredDialog {
    id: root

    // Staged files: list of {url, name, edited} maps. `url` is the file:// URL
    // the send path wants; `name` is the display basename. An edited file points
    // at the baked copy in the cache rather than the file that was picked.
    property var files: []
    property string forcedKind: ""
    property bool viewOnceArmed: false
    // "standard" holds photos to 1600px on the long side, "hd" sends the file's
    // own pixels. One choice for the whole batch, which is what the daemon's
    // send.media_batch takes.
    property string quality: "standard"

    title: Whatevr.I18n.i18nc("@title:dialog confirm attachments before sending", "Send files")
    standardButtons: Kirigami.Dialog.NoButton
    padding: Kirigami.Units.largeSpacing
    preferredWidth: Kirigami.Units.gridUnit * 24
    maximumHeight: Kirigami.Units.gridUnit * 28

    signal confirmed(var fileUrls, string caption, string kind, bool viewOnce, string quality)

    function stage(urls, kind, viewOnce) {
        const staged = []
        for (let i = 0; i < urls.length; ++i) {
            const raw = String(urls[i] || "")
            if (!raw) {
                continue
            }
            let name = raw.substring(raw.lastIndexOf("/") + 1)
            try {
                name = decodeURI(name)
            } catch (e) {
            }
            staged.push({ url: raw, name: name.length > 0 ? name : raw, edited: false })
        }
        root.files = staged
        root.forcedKind = kind || ""
        root.viewOnceArmed = viewOnce === true
        root.quality = "standard"
        captionField.text = ""
        open()
    }

    function isImageName(name) {
        return /\.(png|jpe?g|gif|webp|bmp|svg)$/i.test(name)
    }

    function isVideoName(name) {
        return /\.(mp4|mov|m4v|webm|3gp|mkv|avi|mpg|mpeg)$/i.test(name)
    }

    // Whether anything in the batch is a photo or a clip, which is the only
    // thing the quality choice and the editor apply to.
    readonly property bool hasVisual: {
        for (let i = 0; i < files.length; ++i) {
            const name = String(files[i].name || "")
            if (isImageName(name) || isVideoName(name)) {
                return true
            }
        }
        return false
    }

    // Puts the baked copy in place of the file that was picked. The name keeps
    // the original's stem and gains the extension the editor wrote, so the row
    // still reads as a photo and the thumbnail still loads.
    function replaceWithEdited(index, path) {
        if (!path || path.length === 0 || index < 0 || index >= files.length) {
            return
        }
        const kept = root.files.slice()
        const stem = String(kept[index].name || "").replace(/\.[^.]*$/, "")
        const extension = path.substring(path.lastIndexOf(".") + 1)
        kept[index] = {
            url: Whatevr.ProtocolController.localFileUrl(path).toString(),
            name: (stem.length > 0 ? stem : "image") + "." + extension,
            edited: true
        }
        root.files = kept
    }

    ColumnLayout {
        implicitWidth: root.preferredWidth
        spacing: Kirigami.Units.smallSpacing

        Label {
            visible: root.files.length === 0
            text: Whatevr.I18n.i18nc("@info:placeholder no files staged", "No files selected.")
            color: Kirigami.Theme.disabledTextColor
            Layout.fillWidth: true
        }

        ListView {
            visible: root.files.length > 0
            model: root.files
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(contentHeight, Kirigami.Units.gridUnit * 12)
            clip: true
            spacing: Kirigami.Units.smallSpacing

            delegate: ItemDelegate {
                id: fileDelegate

                required property var modelData
                required property int index

                width: ListView.view.width
                hoverEnabled: false

                contentItem: RowLayout {
                    spacing: Kirigami.Units.smallSpacing

                    // Photo preview for images, generic icon otherwise.
                    Image {
                        Layout.preferredWidth: Kirigami.Units.gridUnit * 2.5
                        Layout.preferredHeight: Kirigami.Units.gridUnit * 2.5
                        source: root.isImageName(String(fileDelegate.modelData.name || ""))
                                ? fileDelegate.modelData.url : ""
                        fillMode: Image.PreserveAspectCrop
                        asynchronous: true
                        cache: true

                        Kirigami.Icon {
                            visible: !root.isImageName(String(fileDelegate.modelData.name || ""))
                            anchors.centerIn: parent
                            source: "document-open-symbolic"
                            width: Kirigami.Units.iconSizes.medium
                            height: width
                        }
                    }

                    Label {
                        text: String(fileDelegate.modelData.name || "")
                        elide: Text.ElideMiddle
                        Layout.fillWidth: true
                    }

                    ToolButton {
                        // Edit a photo or a clip before it goes: a quarter turn,
                        // a crop, something drawn on it. Hidden for documents,
                        // which have nothing to look at. Size is not here — that
                        // is the quality choice below, which the daemon applies
                        // to every kind.
                        visible: root.hasVisual
                                 && (root.isImageName(String(fileDelegate.modelData.name || ""))
                                     || root.isVideoName(String(fileDelegate.modelData.name || "")))
                        icon.name: fileDelegate.modelData.edited === true
                                   ? "document-edit-symbolic" : "edit-paste-symbolic"
                        text: fileDelegate.modelData.edited === true
                              ? Whatevr.I18n.i18nc("@action:button edit a staged file again", "Edit Again")
                              : Whatevr.I18n.i18nc("@action:button edit a staged file", "Edit")
                        display: AbstractButton.IconOnly
                        onClicked: editDialog.editRow(fileDelegate.index,
                                                       fileDelegate.modelData.url,
                                                       fileDelegate.modelData.name)

                        ToolTip.visible: hovered
                        ToolTip.text: text
                        ToolTip.delay: Kirigami.Units.toolTipDelay
                    }

                    ToolButton {
                        icon.name: "list-remove-symbolic"
                        text: Whatevr.I18n.i18nc("@action:button remove a staged file", "Remove")
                        display: AbstractButton.IconOnly
                        onClicked: {
                            const kept = root.files.slice()
                            kept.splice(fileDelegate.index, 1)
                            root.files = kept
                            if (kept.length === 0) {
                                root.close()
                            }
                        }

                        ToolTip.visible: hovered
                        ToolTip.text: text
                        ToolTip.delay: Kirigami.Units.toolTipDelay
                    }
                }
            }
        }

        TextField {
            id: captionField

            Layout.fillWidth: true
            placeholderText: Whatevr.I18n.i18nc("@info:placeholder caption for staged files", "Add a caption…")
        }

        // Standard or HD, for the whole batch. Only shown when there is a photo
        // or a clip in it: it changes nothing for a document.
        RowLayout {
            Layout.fillWidth: true
            visible: root.hasVisual
            spacing: Kirigami.Units.smallSpacing

            Label {
                text: Whatevr.I18n.i18nc("@label quality of what is being sent", "Quality:")
            }

            ComboBox {
                id: qualityBox
                Layout.preferredWidth: Kirigami.Units.gridUnit * 8
                model: [Whatevr.I18n.i18nc("@item:inlist", "Standard"),
                        Whatevr.I18n.i18nc("@item:inlist", "HD")]
                currentIndex: root.quality === "hd" ? 1 : 0
                onActivated: index => root.quality = index === 1 ? "hd" : "standard"
            }

            Label {
                Layout.fillWidth: true
                text: root.quality === "hd"
                    ? Whatevr.I18n.i18nc("@info", "Sent at the original size")
                    : Whatevr.I18n.i18nc("@info", "Shrunk to fit 1600px, as most photos are sent")
                color: Kirigami.Theme.disabledTextColor
                elide: Text.ElideRight
            }
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            Item {
                Layout.fillWidth: true
            }

            Button {
                text: Whatevr.I18n.i18nc("@action:button cancel sending staged files", "Cancel")
                onClicked: root.close()
            }

            Button {
                text: Whatevr.I18n.i18nc("@action:button send staged files", "Send")
                enabled: root.files.length > 0
                highlighted: true
                onClicked: {
                    const urls = root.files.map(f => f.url)
                    const caption = captionField.text
                    const kind = root.forcedKind
                    const once = root.viewOnceArmed
                    const quality = root.quality
                    root.close()
                    root.confirmed(urls, caption, kind, once, quality)
                }
            }
        }
    }

    // The editor, opened on the row that asked for it. It answers with the path
    // of the baked file, which goes into that row in place of the picked one.
    MediaEditDialog {
        id: editDialog

        // The row being edited hands over its index alongside the file, so the
        // answer goes back to the right one when several are opened in turn.
        property int targetIndex: -1

        function editRow(index, url, name) {
            targetIndex = index
            openFor(url, name)
        }

        onEdited: path => root.replaceWithEdited(editDialog.targetIndex, path)
    }
}
