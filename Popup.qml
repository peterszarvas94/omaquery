import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import Quickshell.Hyprland
import qs.Commons as Commons
import qs.Ui as Ui

Item {
    id: root
    property var request: null
    property bool submitting: false
    readonly property bool opened: request !== null
    readonly property int cardPadding: Commons.Style.spacing.panelPadding

    function clear() {
        for (var i = 0; i < fields.count; i++) {
            var row = fields.itemAt(i)
            if (row) row.clear()
        }
        request = null
        submitting = false
    }
    function reply(cancelled) {
        if (!request || submitting) return
        var values = {}
        if (!cancelled) {
            for (var i = 0; i < fields.count; i++) {
                var row = fields.itemAt(i)
                if (!row.value.length) {
                    row.focusInput()
                    return
                }
                values[request.fields[i].name] = row.value
            }
        }
        submitting = true
        bridge.write(JSON.stringify({id: request.id, cancelled: cancelled, values: values}) + "\n")
        // Wait for the bridge's closed acknowledgement before showing another
        // request. Never retain submitted field values in the hidden popup.
        for (var j = 0; j < fields.count; j++) fields.itemAt(j).clear()
    }
    function receive(line) {
        var message
        try { message = JSON.parse(line) } catch (e) { return }
        if (message.type === "closed") {
            if (request && request.id === message.id) clear()
            return
        }
        if (message.type !== "request") return
        clear()
        request = message
        focusTimer.restart()
    }
    function focusFirst() {
        if (opened && fields.count > 0 && fields.itemAt(0)) fields.itemAt(0).focusInput()
    }
    Keys.onEscapePressed: reply(true)
    Process {
        id: bridge
        command: ["omaquery", "serve"]
        stdinEnabled: true
        running: true
        stdout: SplitParser { onRead: line => root.receive(line) }
        onExited: {
            root.clear()
            restartTimer.restart()
        }
    }
    Timer { id: restartTimer; interval: 3000; onTriggered: bridge.running = true }
    Timer {
        id: focusTimer
        interval: 100
        onTriggered: root.focusFirst()
    }
    PanelWindow {
        id: panel
        visible: root.opened
        screen: Quickshell.screens.find(s => Hyprland.focusedMonitor && s.name === Hyprland.focusedMonitor.name) || Quickshell.screens[0]
        implicitWidth: Math.min(Commons.Style.space(560), screen ? screen.width - Commons.Style.space(48) : Commons.Style.space(560))
        readonly property int contentInset: root.cardPadding + 2
        implicitHeight: Math.min(content.implicitHeight + contentInset * 2 + Commons.Style.spacing.xxl, screen ? screen.height - Commons.Style.space(80) : Commons.Style.space(700))
        color: "transparent"
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.namespace: "omaquery"
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.keyboardFocus: root.opened ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
        onBackingWindowVisibleChanged: if (backingWindowVisible) focusTimer.restart()

        Ui.BorderSurface {
            id: card
            anchors.fill: parent
            radius: Commons.Style.cornerRadius
            color: Commons.Color.popups.background
            borderSpec: Commons.Border.surfaceSpec("popups", "border", Commons.Color.popups.border, Math.max(1, Commons.Style.space(2)))

            ScrollView {
                id: scroll
                anchors.fill: parent
                anchors.margins: panel.contentInset
                clip: true
                contentWidth: availableWidth
                contentHeight: content.implicitHeight
                ScrollBar.horizontal.policy: ScrollBar.AlwaysOff
                ScrollBar.vertical.policy: ScrollBar.AsNeeded

                ColumnLayout {
                    id: content
                    width: scroll.availableWidth
                    spacing: Commons.Style.spacing.panelGap

                    Ui.PanelHero {
                        Layout.fillWidth: true
                        title: "omaquery"
                        meta: "Command environment values"
                        detail: root.request && root.request.origin ? "Remote" : "Local"
                        iconComponent: Component {
                            Text {
                                text: "\uf023"
                                color: Commons.Color.accent
                                font.family: Commons.Style.font.menuFamily
                                font.pixelSize: Commons.Style.font.iconLarge
                                verticalAlignment: Text.AlignVCenter
                            }
                        }
                    }

                    Ui.PanelSeparator { Layout.fillWidth: true }

                    Text {
                        Layout.fillWidth: true
                        text: "A command is requesting values for its environment."
                        textFormat: Text.PlainText
                        color: Commons.Color.muted
                        wrapMode: Text.Wrap
                        font.family: Commons.Style.font.family
                        font.pixelSize: Commons.Style.font.body
                    }

                    Text {
                        visible: !!root.request && !!root.request.origin
                        Layout.fillWidth: true
                        text: root.request && root.request.origin ? "Remote request from " + root.request.origin : ""
                        textFormat: Text.PlainText
                        color: Commons.Color.accent
                        wrapMode: Text.WrapAnywhere
                        font.family: Commons.Style.font.family
                        font.pixelSize: Commons.Style.font.bodySmall
                    }

                    Ui.BorderSurface {
                        Layout.fillWidth: true
                        implicitHeight: details.implicitHeight + Commons.Style.spacing.xxl * 2
                        color: Commons.Style.normalFillFor(Commons.Color.foreground, Commons.Color.accent)
                        borderSpec: Commons.Border.controlSpec("normal", Commons.Color.foreground, Commons.Color.accent)
                        radius: Commons.Style.cornerRadius

                        ColumnLayout {
                            id: details
                            anchors.fill: parent
                            anchors.margins: Commons.Style.spacing.xxl
                            spacing: Commons.Style.spacing.sm

                            Ui.PanelSectionHeader {
                                text: "WORKING DIRECTORY"
                                foreground: Commons.Color.foreground
                                fontFamily: Commons.Style.font.family
                            }
                            Text {
                                Layout.fillWidth: true
                                text: root.request ? root.request.cwd : ""
                                textFormat: Text.PlainText
                                color: Commons.Color.muted
                                wrapMode: Text.WrapAnywhere
                                font.family: Commons.Style.font.family
                                font.pixelSize: Commons.Style.font.bodySmall
                            }
                            Ui.PanelSectionHeader {
                                text: "COMMAND"
                                foreground: Commons.Color.foreground
                                fontFamily: Commons.Style.font.family
                            }
                            Text {
                                Layout.fillWidth: true
                                // JSON quoting makes argument boundaries visible,
                                // including whitespace, newlines, and shell syntax.
                                text: root.request ? root.request.command.map(a => JSON.stringify(a)).join(" ") : ""
                                textFormat: Text.PlainText
                                color: Commons.Color.foreground
                                wrapMode: Text.WrapAnywhere
                                font.family: Commons.Style.font.family
                                font.pixelSize: Commons.Style.font.body
                            }
                        }
                    }

                    ColumnLayout {
                        visible: !!root.request && !!(root.request.reason || "")
                        Layout.fillWidth: true
                        spacing: Commons.Style.spacing.labelGap

                        Ui.PanelSectionHeader {
                            text: "REASON"
                            foreground: Commons.Color.foreground
                            fontFamily: Commons.Style.font.family
                        }
                        Text {
                            Layout.fillWidth: true
                            text: root.request ? root.request.reason || "" : ""
                            textFormat: Text.PlainText
                            color: Commons.Color.foreground
                            wrapMode: Text.Wrap
                            font.family: Commons.Style.font.family
                            font.pixelSize: Commons.Style.font.body
                        }
                    }

                    Ui.PanelSeparator {
                        Layout.fillWidth: true
                        visible: fields.count > 0
                    }

                    Repeater {
                        id: fields
                        model: root.request ? root.request.fields : []
                        delegate: ColumnLayout {
                            id: fieldRow
                            required property var modelData
                            required property int index
                            property alias value: input.text
                            property bool revealed: false
                            Layout.fillWidth: true
                            spacing: Commons.Style.spacing.labelGap
                            function clear() { input.text = ""; revealed = false }
                            function focusInput() { input.forceActiveFocus() }
                            Ui.PanelSectionHeader {
                                text: fieldRow.modelData.name
                                foreground: Commons.Color.foreground
                                fontFamily: Commons.Style.font.family
                            }
                            Text {
                                visible: text.length > 0
                                Layout.fillWidth: true
                                text: fieldRow.modelData.hint || ""
                                textFormat: Text.PlainText
                                color: Commons.Color.muted
                                wrapMode: Text.Wrap
                                font.family: Commons.Style.font.family
                                font.pixelSize: Commons.Style.font.bodySmall
                            }
                            RowLayout {
                                Layout.fillWidth: true
                                spacing: Commons.Style.spacing.controlGap
                                Ui.TextField {
                                    id: input
                                    Layout.fillWidth: true
                                    enabled: !root.submitting
                                    password: !fieldRow.revealed
                                    placeholderText: "Enter or paste value"
                                    foreground: Commons.Color.foreground
                                    accent: Commons.Color.accent
                                    font.family: Commons.Style.font.family
                                    font.pixelSize: Commons.Style.font.body
                                    inputMethodHints: fieldRow.revealed
                                        ? Qt.ImhNoPredictiveText
                                        : (Qt.ImhHiddenText | Qt.ImhSensitiveData | Qt.ImhNoPredictiveText)
                                    onAccepted: {
                                        if (fieldRow.index + 1 < fields.count) fields.itemAt(fieldRow.index + 1).focusInput()
                                        else root.reply(false)
                                    }
                                    Keys.onEscapePressed: root.reply(true)
                                }
                                Ui.Button {
                                    text: fieldRow.revealed ? "Hide" : "Show"
                                    focusable: true
                                    bordered: true
                                    tooltipText: fieldRow.revealed ? "Hide this value" : "Show this value"
                                    enabled: !root.submitting
                                    onClicked: fieldRow.revealed = !fieldRow.revealed
                                    Keys.onEscapePressed: root.reply(true)
                                }
                            }
                        }
                    }

                    Text {
                        Layout.fillWidth: true
                        text: "Values go to this command. Its output may expose them."
                        color: Commons.Color.muted
                        wrapMode: Text.Wrap
                        font.family: Commons.Style.font.family
                        font.pixelSize: Commons.Style.font.caption
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Commons.Style.spacing.controlGap
                        Ui.Button {
                            text: "Cancel"
                            focusable: true
                            bordered: true
                            enabled: !root.submitting
                            onClicked: root.reply(true)
                            Keys.onEscapePressed: root.reply(true)
                        }
                        Item { Layout.fillWidth: true }
                        Ui.Button {
                            text: root.submitting ? "Submitting…" : "Run command"
                            focusable: true
                            bordered: true
                            active: true
                            enabled: !root.submitting
                            onClicked: root.reply(false)
                            Keys.onEscapePressed: root.reply(true)
                        }
                    }

                }
            }
        }
    }
}
