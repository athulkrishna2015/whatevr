#pragma once

#include <QObject>
#include <QProcess>
#include <QString>
#include <QUrl>
#include <QVariant>
#include <QVariantMap>
#include <qqmlintegration.h>

// The two halves of the pre-send editor that QML cannot do on its own.
//
// Turning a clip: a quarter turn is a re-encode, so that shells out to ffmpeg.
// That is the same tool the daemon uses for poster frames, and already a hard
// dependency of this frontend through libmpv.
//
// Painting on a photo: crop and rotate are KQuickImageEditor's job (see
// MediaEditDialog.qml), but drawing a freehand mark is a QPainter path and
// nothing more, so it is done here where the pixels are.
//
// Deliberately not a general editor. The crop, the quarter turn, a squiggle and
// a resolution choice are what people actually want before sending a photo to
// someone, and each is a few lines. The daemon owns size (it downscales a
// standard send for every kind), so there is no second opinion about it here.
class MediaEditor final : public QObject
{
    Q_OBJECT
    QML_NAMED_ELEMENT(MediaEditor)
    QML_SINGLETON

    Q_PROPERTY(bool busy READ isBusy NOTIFY busyChanged FINAL)

public:
    explicit MediaEditor(QObject *parent = nullptr);

    // The long side a "standard" quality send holds the file to, which the send
    // dialog labels its control with. The daemon is what enforces it.
    Q_INVOKABLE static int standardMaxSide() { return 1600; }

    /// Re-encodes a clip with a quarter turn clockwise. Answers through
    /// finished()/failed() rather than a return value, because a clip takes
    /// seconds and the caller must not block on it. `degrees` is 0/90/180/270;
    /// 0 comes straight back as an empty path, which the caller reads as "keep
    /// the file you already have".
    Q_INVOKABLE void rotateVideo(const QUrl &source, int degrees);

    /// The pen. Strokes are [{color, width, points: [[x, y], ...]}] in 0..1 of
    /// the image, so one recipe means the same thing on the dialog's preview
    /// and on the photo. Returns a copy, leaving `image` alone, because the
    /// QML side still holds the original for a redo.
    Q_INVOKABLE QVariant paintStrokes(const QVariant &image, const QVariantList &strokes);

    /// Writes an image into the cache and answers with the path, which is what
    /// gets staged in place of the picked file. JPEG for a photo, PNG only where
    /// transparency is real, since a photo saved as PNG is several times the
    /// bytes for no visible gain. Empty string when the write failed.
    Q_INVOKABLE QString saveImage(const QVariant &image);

    Q_INVOKABLE bool isBusy() const { return m_busy; }
    // Abandons a running clip, leaving the caller to send the original.
    Q_INVOKABLE void cancel();

Q_SIGNALS:
    void busyChanged();
    // The local path of the re-encoded clip, or empty when nothing was asked for.
    void finished(const QString &path);
    void failed(const QString &error);

private:
    void setBusy(bool busy);
    void failVideo(const QString &error);

    QProcess *m_process = nullptr;
    bool m_busy = false;
};
