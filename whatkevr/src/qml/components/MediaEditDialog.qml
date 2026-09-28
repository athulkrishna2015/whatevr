pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.kquickimageeditor as KQuickImageEditor
import Whatevr as Whatevr

// The pre-send editor: a quarter turn, a crop, something drawn on it, applied
// to a photo or a clip the user picked.
//
// The picked file is never touched. Crop and rotate are KQuickImageEditor's —
// the same cropper NeoChat and Photos use, with the selection, the handles and
// the undo stack already working — and the result is written to a new file in
// the cache, which is what gets staged in its place. So Cancel here costs a
// quarter turn and a squiggle, not the original.
//
// A clip can be turned and nothing else: there is no frame to crop or draw on
// until it has been re-encoded, and a live preview of a file that is about to
// be re-encoded is a second player in a dialog. Orientation can be checked
// first in the viewer's own rotate, which costs no re-encode at all.
CenteredDialog {
    id: root

    /// The picked file, set before open().
    property url source
    property string fileName: ""

    // Freehand marks, in 0..1 of the picture so the recipe means the same thing
    // whatever size this dialog happens to be.
    property var strokes: []
    property string brushColor: "#ff2d2d"
    property real brushWidth: 0.01
    property bool cropping: false
    property bool drawing: false
    property string errorText: ""

    signal edited(string path)

    readonly property bool isVideo: /\.(mp4|mov|m4v|webm|3gp|mkv|avi|mpg|mpeg)$/i.test(root.fileName)
    readonly property bool havePhoto: doc.image.width > 0

    function openFor(url, name) {
        root.source = url
        root.fileName = String(name || "")
        root.strokes = []
        root.cropping = false
        root.drawing = false
        root.errorText = ""
        open()
        // After open(), so the dialog has its final size and the image is fitted
        // to it rather than laid out once at zero and never measured again.
        Qt.callLater(root.loadSource)
    }

    function loadSource() {
        // Re-setting the path is what reloads the file and clears the document's
        // history, so a second visit to the same file starts clean rather than
        // offering the previous edit's undo steps.
        doc.cancel()
        doc.path = root.source
        doc.edited = false
        selection.aspectRatio = KQuickImageEditor.SelectionTool.AspectRatio.Free
        root.resetSelection()
        ink.requestPaint()
    }

    // The crop rectangle, full picture. Deferred to the image actually having
    // loaded: a selection measured against a picture that has not arrived yet
    // is a selection of nothing.
    function resetSelection() {
        if (editImage.paintedWidth <= 0) {
            return
        }
        selection.selectionX = 0
        selection.selectionY = 0
        selection.selectionWidth = editImage.paintedWidth
        selection.selectionHeight = editImage.paintedHeight
    }

    function apply() {
        if (root.isVideo) {
            // The turn is the only edit a clip gets, and the viewer's own
            // rotation is not involved: this one goes into the file.
            if (root.quarterTurns === 0) {
                root.close()
                root.edited("")
                return
            }
            Whatevr.MediaEditor.rotateVideo(root.source, root.quarterTurns)
            return
        }
        // `doc.edited` rather than a count of turn presses, because Undo takes
        // it back down and a counter would not.
        if (!doc.edited && root.strokes.length === 0) {
            // Nothing to bake. An empty path tells the caller to keep the file it
            // already has, rather than to re-encode a photo that was not changed.
            root.close()
            root.edited("")
            return
        }
        if (root.cropping && editImage.ratioX > 0 && editImage.ratioY > 0) {
            // The selection is in painted pixels; the document wants the
            // picture's own, which is what the ratio undoes.
            doc.crop(Math.round(selection.selectionX / editImage.ratioX),
                     Math.round(selection.selectionY / editImage.ratioY),
                     Math.round(selection.selectionWidth / editImage.ratioX),
                     Math.round(selection.selectionHeight / editImage.ratioY))
        }
        const painted = Whatevr.MediaEditor.paintStrokes(doc.image, root.strokes)
        const path = painted ? Whatevr.MediaEditor.saveImage(painted) : ""
        if (path.length === 0) {
            root.errorText = Whatevr.I18n.i18nc("@info", "The edited image could not be written")
            return
        }
        root.close()
        root.edited(path)
    }

    // Kept as a count of quarter turns rather than an angle, so two rights and
    // one left is one right, and a photo turned upside down is two rights
    // rather than an angle nothing else in the app uses.
    property int turns: 0
    readonly property int quarterTurns: ((root.turns % 4) + 4) % 4 * 90

    Connections {
        target: Whatevr.MediaEditor
        function onFinished(path) {
            root.close()
            root.edited(path)
        }
        function onFailed(error) {
            // Nothing was written to the picked file, so the edit is simply not
            // there and the original can still be sent.
            root.errorText = error
        }
    }

    title: Whatevr.I18n.i18nc("@title:dialog edit an attachment before sending", "Edit")
    standardButtons: Kirigami.Dialog.NoButton
    padding: Kirigami.Units.largeSpacing
    preferredWidth: Kirigami.Units.gridUnit * 26
    maximumHeight: Kirigami.Units.gridUnit * 30

    // The picture, as it will go out. ImageDocument holds the edits and the
    // undo stack, ImageItem draws the current result, and SelectionTool is the
    // crop rectangle over the top of it.
    //
    // A QObject rather than an Item, so it sits here and not in the layout.
    KQuickImageEditor.ImageDocument {
        id: doc
    }

    ColumnLayout {
        implicitWidth: root.preferredWidth - root.padding * 2
        spacing: Kirigami.Units.smallSpacing

        Item {
            id: stage

            Layout.fillWidth: true
            Layout.preferredHeight: Kirigami.Units.gridUnit * 13
            clip: true

            KQuickImageEditor.ImageItem {
                id: editImage

                // Painted size over native size: the factor that turns the crop
                // selection's pixels back into the picture's own.
                readonly property real ratioX: paintedWidth / Math.max(1, nativeWidth)
                readonly property real ratioY: paintedHeight / Math.max(1, nativeHeight)

                anchors.fill: parent
                anchors.margins: Kirigami.Units.gridUnit
                fillMode: KQuickImageEditor.ImageItem.PreserveAspectFit
                image: doc.image

                // The crop rectangle has to wait for a picture to measure
                // against, and a turn or a crop changes that picture.
                onPaintedWidthChanged: root.resetSelection()
                onImageChanged: root.resetSelection()

                KQuickImageEditor.SelectionTool {
                    id: selection

                    visible: root.cropping
                    // The selection is measured against the drawn picture, not
                    // the whole item: the letterbox either side of a photo that
                    // does not fill the frame is not part of the picture.
                    width: editImage.paintedWidth
                    height: editImage.paintedHeight
                    x: editImage.horizontalPadding
                    y: editImage.verticalPadding

                    KQuickImageEditor.CropBackground {
                        anchors.fill: parent
                        z: -1
                        insideX: selection.selectionX
                        insideY: selection.selectionY
                        insideWidth: selection.selectionWidth
                        insideHeight: selection.selectionHeight
                    }

                    // A double tap inside the crop is the other way to leave crop
                    // mode, the way every other cropper works. The rectangle
                    // stays where it was put, and Apply is what takes it.
                    Connections {
                        target: selection.selectionArea
                        function onDoubleClicked() {
                            root.cropping = false
                        }
                    }
                }

                // The drawn marks. A Canvas over the picture, painting the same
                // recipe the editor bakes, so what is on screen is what is
                // sent. KQuickImageEditor's annotation system is the other way to
                // draw, but it composes onto a fixed canvas rather than turning
                // one, which is the wrong model for editing a photo to send.
                Canvas {
                    id: ink

                    anchors.fill: parent
                    visible: root.strokes.length > 0
                    onPaint: {
                        const ctx = getContext("2d")
                        ctx.reset()
                        ctx.lineCap = "round"
                        ctx.lineJoin = "round"
                        for (let i = 0; i < root.strokes.length; ++i) {
                            const stroke = root.strokes[i]
                            const points = stroke.points
                            if (points.length === 0)
                                continue
                            ctx.strokeStyle = stroke.color
                            ctx.lineWidth = stroke.width * width
                            const firstX = points[0][0] * width
                            const firstY = points[0][1] * height
                            if (points.length === 1) {
                                ctx.fillStyle = stroke.color
                                ctx.beginPath()
                                ctx.arc(firstX, firstY, ctx.lineWidth / 2, 0, 2 * Math.PI)
                                ctx.fill()
                                continue
                            }
                            ctx.beginPath()
                            ctx.moveTo(firstX, firstY)
                            for (let p = 1; p < points.length; ++p) {
                                ctx.lineTo(points[p][0] * width, points[p][1] * height)
                            }
                            ctx.stroke()
                        }
                    }
                }

                // The pen, over whatever the crop is not using. Anchored to the
                // drawn picture so a mark lands where the pointer is rather than
                // where the letterbox is.
                MouseArea {
                    id: pen

                    x: editImage.horizontalPadding
                    y: editImage.verticalPadding
                    width: editImage.paintedWidth
                    height: editImage.paintedHeight
                    visible: root.drawing
                    enabled: root.drawing
                    preventStealing: true
                    property var stroke: null

                    onPressed: mouse => {
                        pen.stroke = { color: root.brushColor, width: root.brushWidth,
                                       points: [[mouse.x / width, mouse.y / height]] }
                        // Pushed once and then grown in place. Reassigning the
                        // whole array on every mouse move would copy it per
                        // point, which a single drag across a photo makes
                        // hundreds of times over.
                        root.strokes = root.strokes.concat([pen.stroke])
                        ink.requestPaint()
                    }
                    onPositionChanged: mouse => {
                        if (!pressed || !pen.stroke)
                            return
                        const u = mouse.x / width
                        const v = mouse.y / height
                        if (u < 0 || u > 1 || v < 0 || v > 1)
                            return
                        pen.stroke.points.push([u, v])
                        ink.requestPaint()
                    }
                }
            }

            // A clip has no picture to show, so it says what it is rather than
            // presenting an empty frame.
            ColumnLayout {
                anchors.centerIn: parent
                visible: !root.isVideo && !root.havePhoto
                width: Math.min(parent.width - Kirigami.Units.gridUnit * 2, Kirigami.Units.gridUnit * 20)
                spacing: Kirigami.Units.smallSpacing

                Kirigami.Icon {
                    Layout.alignment: Qt.AlignHCenter
                    source: "image-missing-symbolic"
                    implicitWidth: Kirigami.Units.iconSizes.large
                    implicitHeight: implicitWidth
                }

                QQC2.Label {
                    Layout.fillWidth: true
                    horizontalAlignment: Text.AlignHCenter
                    wrapMode: Text.Wrap
                    color: Kirigami.Theme.disabledTextColor
                    text: Whatevr.I18n.i18nc("@info", "That image could not be opened")
                }
            }
        }

        // Crop and draw are a radio pair: both are drags over the same pixels,
        // and doing both at once is neither legible nor useful.
        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            QQC2.ToolButton {
                icon.name: "object-rotate-left-symbolic"
                text: Whatevr.I18n.i18nc("@action:button", "Rotate Left")
                display: QQC2.AbstractButton.IconOnly
                focusPolicy: Qt.NoFocus
                onClicked: {
                    doc.rotate(-90)
                    root.turns = root.turns - 1
                }
                QQC2.ToolTip.text: text
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
                Accessible.name: text
            }

            QQC2.ToolButton {
                icon.name: "object-rotate-right-symbolic"
                text: Whatevr.I18n.i18nc("@action:button", "Rotate Right")
                display: QQC2.AbstractButton.IconOnly
                focusPolicy: Qt.NoFocus
                onClicked: {
                    doc.rotate(90)
                    root.turns = root.turns + 1
                }
                QQC2.ToolTip.text: text
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
                Accessible.name: text
            }

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Crop")
                checkable: true
                checked: root.cropping
                focusPolicy: Qt.NoFocus
                enabled: root.havePhoto
                onToggled: {
                    root.cropping = checked
                    root.drawing = false
                }
            }

            QQC2.ComboBox {
                visible: root.cropping
                focusPolicy: Qt.NoFocus
                // Free or square, because that is what the selection tool locks
                // to. A 4:3 or 16:9 lock is a drag away from a square one.
                model: [Whatevr.I18n.i18nc("@item:inlist", "Free"), "1:1"]
                onActivated: index => selection.aspectRatio = index === 1
                    ? KQuickImageEditor.SelectionTool.AspectRatio.Square
                    : KQuickImageEditor.SelectionTool.AspectRatio.Free
            }

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Draw")
                checkable: true
                checked: root.drawing
                focusPolicy: Qt.NoFocus
                enabled: root.havePhoto
                onToggled: {
                    root.drawing = checked
                    root.cropping = false
                }
            }

            QQC2.ToolButton {
                icon.name: "edit-undo-symbolic"
                text: Whatevr.I18n.i18nc("@action:button", "Undo")
                display: QQC2.AbstractButton.IconOnly
                focusPolicy: Qt.NoFocus
                enabled: doc.edited
                onClicked: doc.undo()
                QQC2.ToolTip.text: text
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
                Accessible.name: text
            }

            Item {
                Layout.fillWidth: true
            }
        }

        // Only while drawing, and only the two things a freehand mark needs.
        RowLayout {
            Layout.fillWidth: true
            visible: root.drawing
            spacing: Kirigami.Units.smallSpacing

            Repeater {
                model: ["#ff2d2d", "#ffd02d", "#2dd07d", "#2d7dff", "#ffffff", "#101010"]
                delegate: QQC2.ToolButton {
                    required property string modelData
                    focusPolicy: Qt.NoFocus
                    implicitWidth: Kirigami.Units.gridUnit * 1.6
                    padding: 0
                    background: Rectangle {
                        radius: height / 2
                        color: root.brushColor === modelData ? Kirigami.Theme.highlightColor : modelData
                        border.color: Kirigami.Theme.textColor
                    }
                    onClicked: root.brushColor = modelData
                    QQC2.ToolTip.text: modelData
                    QQC2.ToolTip.visible: hovered
                    QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
                    Accessible.name: modelData
                }
            }

            QQC2.Slider {
                Layout.preferredWidth: Kirigami.Units.gridUnit * 6
                from: 0.002
                to: 0.05
                stepSize: 0.002
                value: root.brushWidth
                onMoved: root.brushWidth = value
            }

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Undo Stroke")
                focusPolicy: Qt.NoFocus
                enabled: root.strokes.length > 0
                onClicked: {
                    root.strokes = root.strokes.slice(0, -1)
                    ink.requestPaint()
                }
            }
        }

        QQC2.BusyIndicator {
            Layout.alignment: Qt.AlignHCenter
            visible: Whatevr.MediaEditor.busy
            running: visible
        }

        QQC2.Label {
            Layout.fillWidth: true
            visible: root.errorText.length > 0
            text: root.errorText
            color: Kirigami.Theme.negativeTextColor
            wrapMode: Text.Wrap
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Reset")
                focusPolicy: Qt.NoFocus
                enabled: doc.edited || root.strokes.length > 0 || root.turns !== 0
                onClicked: {
                    root.turns = 0
                    root.strokes = []
                    doc.cancel()
                    doc.path = ""
                    doc.edited = false
                    ink.requestPaint()
                }
            }

            Item {
                Layout.fillWidth: true
            }

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Cancel")
                focusPolicy: Qt.NoFocus
                onClicked: root.close()
            }

            QQC2.Button {
                text: Whatevr.I18n.i18nc("@action:button", "Apply")
                highlighted: true
                focusPolicy: Qt.NoFocus
                enabled: !Whatevr.MediaEditor.busy
                onClicked: root.apply()
            }
        }
    }
}
