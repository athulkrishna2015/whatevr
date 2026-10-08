#pragma once

#include <QMenu>
#include <QObject>
#include <QPoint>
#include <QSystemTrayIcon>

#include <qqmlintegration.h>

// Frontend-owned tray icon. The v1 daemon exported the tray icon itself and
// the UI only drew its menu; the v2 daemon has no tray, so the frontend owns
// the whole thing: icon, activation, and menu. Unread counts are not badged
// (QSystemTrayIcon has no badge API); notifications already surface them.
//
// Not final: the QML registration wrapper derives from this type.
class TrayIcon : public QObject
{
    Q_OBJECT
    QML_NAMED_ELEMENT(TrayIcon)

public:
    explicit TrayIcon(QObject *parent = nullptr);
    ~TrayIcon() override = default;

    // Whether the tray icon is shown. The app keeps it visible for its whole
    // lifetime so close-to-tray always has somewhere to come back to.
    void setTrayVisible(bool visible);

Q_SIGNALS:
    // Left-click or the Show/Hide menu row: toggle the main window.
    void toggleWindowRequested();
    // The Mute notifications menu row (checked = muted).
    void muteNotificationsRequested(bool muted);
    // The Quit menu row.
    void quitRequested();

public Q_SLOTS:
    // Sync the Mute row's check state with the daemon preference.
    void setNotificationsMuted(bool muted);

private:
    void onActivated(QSystemTrayIcon::ActivationReason reason);

    QSystemTrayIcon *m_tray = nullptr;
    QMenu *m_menu = nullptr;
    QAction *m_muteAction = nullptr;
};
