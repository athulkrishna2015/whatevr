#include "trayicon.h"

#include <QApplication>
#include <QIcon>

TrayIcon::TrayIcon(QObject *parent)
    : QObject(parent)
    , m_tray(new QSystemTrayIcon(this))
    , m_menu(new QMenu())
{
    // The installed application icon; fall back to a stock one so a missing
    // install prefix still shows something raisable.
    QIcon icon = QIcon::fromTheme(QStringLiteral("in.codelif.Whatevr"),
                                  QIcon::fromTheme(QStringLiteral("im-chat")));
    m_tray->setIcon(icon);
    m_tray->setToolTip(QStringLiteral("Whatevr"));

    QAction *toggle = m_menu->addAction(QStringLiteral("Show/Hide"));
    connect(toggle, &QAction::triggered, this, &TrayIcon::toggleWindowRequested);
    QAction *quit = m_menu->addAction(QStringLiteral("Quit"));
    connect(quit, &QAction::triggered, this, &TrayIcon::quitRequested);
    m_tray->setContextMenu(m_menu);

    connect(m_tray, &QSystemTrayIcon::activated, this, &TrayIcon::onActivated);
    m_tray->setVisible(true);
}

void TrayIcon::setTrayVisible(bool visible)
{
    if (m_tray) {
        m_tray->setVisible(visible);
    }
}

void TrayIcon::onActivated(QSystemTrayIcon::ActivationReason reason)
{
    switch (reason) {
    case QSystemTrayIcon::Trigger:
    case QSystemTrayIcon::DoubleClick:
    case QSystemTrayIcon::MiddleClick:
        Q_EMIT toggleWindowRequested();
        break;
    case QSystemTrayIcon::Context: {
        const QRect geometry = m_tray->geometry();
        Q_EMIT trayMenuRequested(geometry.isValid() ? geometry.bottomLeft() : QPoint());
        break;
    }
    default:
        break;
    }
}
