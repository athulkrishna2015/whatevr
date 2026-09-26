import QtQuick
import org.kde.kirigamiaddons.formcard as FormCard

import Whatevr as Whatevr

SettingsPage {
    id: page

    title: Whatevr.I18n.i18nc("@title settings category", "Privacy")

    Component.onCompleted: Whatevr.ProtocolController.openPrivacySettings()
    Component.onDestruction: Whatevr.ProtocolController.closePrivacySettings()

    readonly property var everyoneContactsNobody: [
        { value: "all", text: Whatevr.I18n.i18nc("@item privacy audience", "Everyone") },
        { value: "contacts", text: Whatevr.I18n.i18nc("@item privacy audience", "My contacts") },
        { value: "contact_blacklist", text: Whatevr.I18n.i18nc("@item privacy audience", "My contacts except…") },
        { value: "none", text: Whatevr.I18n.i18nc("@item privacy audience", "Nobody") }
    ]
    readonly property var onlineModel: [
        { value: "all", text: Whatevr.I18n.i18nc("@item privacy audience", "Everyone") },
        { value: "match_last_seen", text: Whatevr.I18n.i18nc("@item privacy audience", "Same as last seen") }
    ]
    readonly property var callModel: [
        { value: "all", text: Whatevr.I18n.i18nc("@item privacy audience", "Everyone") },
        { value: "known", text: Whatevr.I18n.i18nc("@item privacy audience", "Known contacts") }
    ]
    readonly property var statusModel: [
        { value: "contacts", text: Whatevr.I18n.i18nc("@item privacy audience", "My contacts") },
        { value: "contact_blacklist", text: Whatevr.I18n.i18nc("@item privacy audience", "My contacts except…") },
        { value: "contact_allowlist", text: Whatevr.I18n.i18nc("@item privacy audience", "Only share with…") }
    ]
    readonly property var defaultTimerModel: [
        { value: 0, text: Whatevr.I18n.i18nc("@item:inlistbox", "Off") },
        { value: 86400, text: Whatevr.I18n.i18nc("@item:inlistbox", "24 hours") },
        { value: 604800, text: Whatevr.I18n.i18nc("@item:inlistbox", "7 days") },
        { value: 7776000, text: Whatevr.I18n.i18nc("@item:inlistbox", "90 days") }
    ]

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Who can see my…")
    }

    FormCard.FormCard {
        PrivacyAudienceCombo {
            objectName: "privacy.lastSeen"
            text: Whatevr.I18n.i18nc("@label:listbox", "Last seen")
            categoryKey: "last_seen"
            audienceModel: page.everyoneContactsNobody
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.online"
            text: Whatevr.I18n.i18nc("@label:listbox", "Online")
            categoryKey: "online"
            audienceModel: page.onlineModel
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.profilePhoto"
            text: Whatevr.I18n.i18nc("@label:listbox", "Profile photo")
            categoryKey: "profile_photo"
            audienceModel: page.everyoneContactsNobody
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.about"
            text: Whatevr.I18n.i18nc("@label:listbox", "About")
            categoryKey: "about"
            audienceModel: page.everyoneContactsNobody
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.status"
            text: Whatevr.I18n.i18nc("@label:listbox", "Status updates")
            description: Whatevr.I18n.i18nc("@info", "Who can see the status updates you post. Shown for reference: change it in WhatsApp on your phone.")
            categoryKey: "status"
            audienceModel: page.statusModel
            enabled: false
        }
    }

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Messaging")
    }

    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            id: readReceiptsSwitch
            objectName: "privacy.readReceipts"
            text: Whatevr.I18n.i18nc("@option:check", "Read receipts")
            description: Whatevr.I18n.i18nc("@info", "When off, you won't send or receive read receipts. Read receipts are always sent in group chats.")
            checked: Whatevr.ProtocolController.privacySettings.read_receipts ?? true
            onToggled: Whatevr.ProtocolController.setReadReceipts(checked)

            // Toggling a switch breaks its `checked` binding, so an external
            // change (e.g. from the phone) would stop reflecting. Re-establish
            // the binding whenever the privacy settings change.
            Connections {
                target: Whatevr.ProtocolController
                function onPrivacySettingsChanged() {
                    readReceiptsSwitch.checked = Qt.binding(() => Whatevr.ProtocolController.privacySettings.read_receipts ?? true)
                }
                function onSettingsActionFailed() {
                    readReceiptsSwitch.checked = Qt.binding(() => Whatevr.ProtocolController.privacySettings.read_receipts ?? true)
                }
            }
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.groupAdd"
            text: Whatevr.I18n.i18nc("@label:listbox", "Who can add me to groups")
            categoryKey: "group_add"
            audienceModel: page.everyoneContactsNobody
        }

        FormCard.FormDelegateSeparator {}

        PrivacyAudienceCombo {
            objectName: "privacy.callAdd"
            text: Whatevr.I18n.i18nc("@label:listbox", "Who can call me")
            categoryKey: "call_add"
            audienceModel: page.callModel
        }
    }

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Disappearing messages")
    }

    FormCard.FormCard {
        FormCard.FormComboBoxDelegate {
            id: defaultTimerCombo
            objectName: "privacy.defaultTimer"
            text: Whatevr.I18n.i18nc("@label:listbox", "Default message timer")
            description: (Whatevr.ProtocolController.privacySettings.default_timer_seconds ?? -1) < 0
                ? Whatevr.I18n.i18nc("@info", "Chats you start from now disappear after this long. Your account did not report its current timer — picking a value sets it.")
                : Whatevr.I18n.i18nc("@info", "Chats you start from now disappear after this long. Chats already running keep their own timer.")
            textRole: "text"
            valueRole: "value"
            model: page.defaultTimerModel

            function syncFromSettings() {
                const configured = Whatevr.ProtocolController.privacySettings.default_timer_seconds ?? -1;
                const idx = configured < 0 ? -1 : defaultTimerCombo.indexOfValue(configured);
                defaultTimerCombo.currentIndex = idx;
            }

            Component.onCompleted: defaultTimerCombo.syncFromSettings()
            onActivated: index => Whatevr.ProtocolController.setDefaultDisappearingTimer(model[index].value)

            Connections {
                target: Whatevr.ProtocolController
                function onPrivacySettingsChanged() { defaultTimerCombo.syncFromSettings(); }
                function onSettingsActionFailed() { defaultTimerCombo.syncFromSettings(); }
            }
        }
    }

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Blocked")
    }

    FormCard.FormCard {
        FormCard.FormButtonDelegate {
            objectName: "privacy.blocked"
            text: Whatevr.I18n.i18nc("@action:button", "Blocked contacts")
            description: Whatevr.I18n.i18nc("@info", "Manage the contacts you have blocked.")
            icon.name: "im-ban-user-symbolic"
            onClicked: page.hostPageStack.push(blockedPageComponent)
        }
    }

    Component {
        id: blockedPageComponent
        BlockedContactsPage {}
    }
}
