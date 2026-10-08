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
    // Left-click: toggle the main window.
    void toggleWindowRequested();
    // Right-click position for the QML tray menu window, which owns the whole
    // menu (show/hide, notifications toggle, quit). No native context menu:
    // it would shadow the QML one with a poorer duplicate.
    void trayMenuRequested(const QPoint &globalPos);
    // Kept for API symmetry; the QML menu's Quit row calls quitApplication().
    void quitRequested();

private:
    void onActivated(QSystemTrayIcon::ActivationReason reason);

    QSystemTrayIcon *m_tray = nullptr;
};
