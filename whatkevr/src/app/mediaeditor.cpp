#include "mediaeditor.h"

#include <QDir>
#include <QFileInfo>
#include <QImage>
#include <QPainter>
#include <QPainterPath>
#include <QPen>
#include <QStandardPaths>
#include <QTimer>
#include <QUuid>

// i18nc, like everywhere else in this app.
#include <KLocalizedString>

namespace
{

// A quarter turn is the only rotation that is lossless, and the only one the
// editor offers. Anything else is normalised to one, so a stray value cannot
// produce a picture on its side.
int normalizeDegrees(int degrees)
{
    const int wrapped = ((degrees % 360) + 360) % 360;
    return (wrapped / 90) * 90;
}

// A private file in the cache, next to the clipboard paste and the contact cards
// the controller already writes there. The daemon copies the bytes itself
// before it answers, so nothing has to keep this one around afterwards.
QString editedFilePath(const QString &suffix)
{
    const QString dir =
        QStandardPaths::writableLocation(QStandardPaths::CacheLocation) + QStringLiteral("/edited");
    QDir().mkpath(dir);
    return QStringLiteral("%1/edit-%2.%3").arg(dir, QUuid::createUuid().toString(QUuid::Id128), suffix);
}

// One freehand stroke. Points and width are fractions of the image rather than
// pixels, so the same recipe reads the same on the dialog's preview and on the
// photo, and a two-pixel pen on a thumbnail is a two-pixel pen on a
// 12-megapixel original rather than a hairline.
void drawStroke(QPainter *painter, const QVariantMap &stroke, const QSize &size)
{
    const QVariantList points = stroke.value(QStringLiteral("points")).toList();
    if (points.isEmpty()) {
        return;
    }
    const double shortest = qMin(size.width(), size.height());
    const qreal width = qMax(1.0, stroke.value(QStringLiteral("width")).toDouble() * shortest);

    QPen pen(QColor(stroke.value(QStringLiteral("color")).toString()));
    pen.setWidthF(width);
    pen.setCapStyle(Qt::RoundCap);
    pen.setJoinStyle(Qt::RoundJoin);
    painter->setPen(pen);

    const QPointF first = [&] {
        const QVariantList point = points.first().toList();
        return QPointF(point.value(0).toDouble() * size.width(),
                       point.value(1).toDouble() * size.height());
    }();

    if (points.size() == 1) {
        // A tap, not a drag. A round cap does not help a path with one point:
        // there is no segment for it to cap.
        painter->drawEllipse(first, width / 2, width / 2);
        return;
    }
    QPainterPath path;
    path.moveTo(first);
    for (int i = 1; i < points.size(); ++i) {
        const QVariantList point = points.at(i).toList();
        path.lineTo(point.value(0).toDouble() * size.width(),
                    point.value(1).toDouble() * size.height());
    }
    painter->drawPath(path);
}

} // namespace

MediaEditor::MediaEditor(QObject *parent)
    : QObject(parent)
{
}

void MediaEditor::setBusy(bool busy)
{
    if (m_busy == busy) {
        return;
    }
    m_busy = busy;
    Q_EMIT busyChanged();
}

QVariant MediaEditor::paintStrokes(const QVariant &image, const QVariantList &strokes)
{
    QImage canvas = image.value<QImage>();
    if (canvas.isNull() || strokes.isEmpty()) {
        return canvas;
    }
    QPainter painter(&canvas);
    painter.setRenderHint(QPainter::Antialiasing, true);
    for (const QVariant &stroke : strokes) {
        drawStroke(&painter, stroke.toMap(), canvas.size());
    }
    return canvas;
}

QString MediaEditor::saveImage(const QVariant &image)
{
    const QImage canvas = image.value<QImage>();
    if (canvas.isNull()) {
        return {};
    }
    // PNG only where transparency is real: a photo saved as PNG is several times
    // the bytes for nothing anyone can see, and WhatsApp recompresses both.
    const bool keepAlpha = canvas.hasAlphaChannel();
    const QString path = editedFilePath(keepAlpha ? QStringLiteral("png") : QStringLiteral("jpg"));
    return canvas.save(path, keepAlpha ? "PNG" : "JPG", 92) ? path : QString();
}

void MediaEditor::rotateVideo(const QUrl &source, int degrees)
{
    const int rotate = normalizeDegrees(degrees);
    if (m_busy) {
        return;
    }
    const QString path = source.isLocalFile() ? source.toLocalFile() : source.toString();
    if (path.isEmpty() || !QFileInfo::exists(path)) {
        Q_EMIT failed(i18nc("@info", "That file is no longer there"));
        return;
    }
    if (rotate == 0) {
        // A quarter turn is the only thing a clip can be given here, and turning
        // it is a re-encode, so there is nothing to do without one.
        Q_EMIT finished(QString());
        return;
    }

    QStringList filters;
    if (rotate == 90) {
        filters << QStringLiteral("transpose=1"); // a quarter turn clockwise
    } else if (rotate == 180) {
        filters << QStringLiteral("transpose=1,transpose=1");
    } else {
        filters << QStringLiteral("transpose=2");
    }
    // A turned clip is a H.264 clip again, so the daemon classifies it as a
    // video and its quality choice still applies to the result.
    const QString out = editedFilePath(QStringLiteral("mp4"));

    auto *process = new QProcess(this);
    m_process = process;
    connect(process, &QProcess::errorOccurred, this, [this](QProcess::ProcessError code) {
        if (code == QProcess::FailedToStart) {
            failVideo(i18nc("@info", "ffmpeg is not installed, so a clip cannot be turned"));
        }
    });
    connect(process, &QProcess::finished, this, [this, process, out](int code, QProcess::ExitStatus status) {
        const QString error = QString::fromUtf8(process->readAllStandardError()).trimmed();
        process->deleteLater();
        if (m_process == process) {
            m_process = nullptr;
        }
        if (status != QProcess::NormalExit || code != 0) {
            failVideo(error.isEmpty() ? i18nc("@info", "The clip could not be re-encoded") : error);
            return;
        }
        // ffmpeg exits 0 having written nothing when the filter chain produced
        // no frames, which is the other way a turn silently does nothing.
        const QFileInfo info(out);
        if (!info.exists() || info.size() == 0) {
            failVideo(i18nc("@info", "The clip could not be re-encoded"));
            return;
        }
        setBusy(false);
        Q_EMIT finished(out);
    });

    const QStringList args{QStringLiteral("-nostdin"),
                           QStringLiteral("-hide_banner"),
                           QStringLiteral("-loglevel"),
                           QStringLiteral("error"),
                           QStringLiteral("-y"),
                           QStringLiteral("-i"),
                           path,
                           QStringLiteral("-vf"),
                           filters.join(QLatin1Char(',')),
                           QStringLiteral("-c:v"),
                           QStringLiteral("libx264"),
                           QStringLiteral("-preset"),
                           QStringLiteral("veryfast"),
                           // Turning a clip throws away most of what was encoded,
                           // so the ceiling is low on purpose: a rotated copy is a
                           // convenience, not an archive.
                           QStringLiteral("-crf"),
                           QStringLiteral("26"),
                           QStringLiteral("-pix_fmt"),
                           QStringLiteral("yuv420p"),
                           QStringLiteral("-c:a"),
                           QStringLiteral("aac"),
                           QStringLiteral("-b:a"),
                           QStringLiteral("96k"),
                           QStringLiteral("-movflags"),
                           QStringLiteral("+faststart"),
                           out};
    process->start(QStringLiteral("ffmpeg"), args);
    // A clip that will not finish is a stuck dialog, which is worse than a
    // failed edit: five minutes, then it is stopped and the original stands.
    QTimer::singleShot(300000, process, [process] {
        if (process->state() != QProcess::NotRunning) {
            process->kill();
        }
    });
}

void MediaEditor::cancel()
{
    if (m_process) {
        m_process->kill();
    }
    setBusy(false);
}

void MediaEditor::failVideo(const QString &error)
{
    if (m_process) {
        m_process->deleteLater();
        m_process = nullptr;
    }
    setBusy(false);
    Q_EMIT failed(error);
}
