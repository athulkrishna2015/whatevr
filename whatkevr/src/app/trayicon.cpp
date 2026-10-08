#include "trayicon.h"

#include <QApplication>
#include <QIcon>
#include <QSignalBlocker>

TrayIcon::TrayIcon(QObject *parent)
    : QObject(parent)
    , m_tray(new QSystemTrayIcon(this))
    , m_menu(new QMenu())
    , m_muteAction(new QAction(m_menu))
{
    // The installed application icon; fall back to a stock one so a missing
    // install prefix still shows something raisable.
    QIcon icon = QIcon::fromTheme(QStringLiteral("in.codelif.Whatevr"),
                                  QIcon::fromTheme(QStringLiteral("im-chat")));
    m_tray->setIcon(icon);
    m_tray->setToolTip(QStringLiteral("Whatevr"));

    // The whole menu is native: the QML tray menu window stays out of it.
    QAction *toggle = m_menu->addAction(QStringLiteral("Show/Hide"));
    connect(toggle, &QAction::triggered, this, &TrayIcon::toggleWindowRequested);
    m_muteAction->setText(QStringLiteral("Mute notifications"));
    m_muteAction->setCheckable(true);
    connect(m_muteAction, &QAction::toggled, this, &TrayIcon::muteNotificationsRequested);
    m_menu->addAction(m_muteAction);
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

void TrayIcon::setNotificationsMuted(bool muted)
{
    if (m_muteAction) {
        const QSignalBlocker blocker(m_muteAction);
        m_muteAction->setChecked(muted);
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
    default:
        // Right-click opens the native context menu above on its own.
        break;
    }
}
