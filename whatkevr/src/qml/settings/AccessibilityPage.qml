import QtQuick
import org.kde.kirigamiaddons.formcard as FormCard

import Whatevr as Whatevr

SettingsPage {
    id: page

    title: Whatevr.I18n.i18nc("@title settings category", "Accessibility")

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Reading")
    }

    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            objectName: "accessibility.increaseContrast"
            text: Whatevr.I18n.i18nc("@option:check", "Increase contrast")
            description: Whatevr.I18n.i18nc("@info", "Use full-strength text for secondary and disabled labels instead of faded ones.")
            checked: Whatevr.Settings.increaseContrast
            onToggled: Whatevr.Settings.increaseContrast = checked
        }
    }

    FormCard.FormHeader {
        title: Whatevr.I18n.i18nc("@title:group", "Movement")
    }

    FormCard.FormCard {
        FormCard.FormSwitchDelegate {
            objectName: "accessibility.reduceMotion"
            text: Whatevr.I18n.i18nc("@option:check", "Reduce motion")
            description: Whatevr.I18n.i18nc("@info", "Prefer still content over animated movement.")
            checked: Whatevr.Settings.reduceMotion
            onToggled: Whatevr.Settings.reduceMotion = checked
        }
    }
}
