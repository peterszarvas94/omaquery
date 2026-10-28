import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import Quickshell.Hyprland
import qs.Commons as Commons

Item {
    id: root
    property var request: null
    property bool submitting: false
    property bool focusPrimed: false
    readonly property bool opened: request !== null

    function clear() {
        for (var i = 0; i < fields.count; i++) {
            var row = fields.itemAt(i)
            if (row) row.clear()
        }
        request = null
        submitting = false
        focusPrimed = false
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
        onTriggered: {
            root.focusFirst()
            root.focusPrimed = true
        }
    }
    PanelWindow {
        id: panel
        visible: root.opened
        screen: Quickshell.screens.find(s => Hyprland.focusedMonitor && s.name === Hyprland.focusedMonitor.name) || Quickshell.screens[0]
        implicitWidth: Math.min(560, screen ? screen.width - 48 : 560)
        implicitHeight: Math.min(content.implicitHeight + 48, screen ? screen.height - 80 : 700)
        color: "transparent"
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.namespace: "omaquery"
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.keyboardFocus: !root.opened ? WlrKeyboardFocus.None :
            (root.focusPrimed ? WlrKeyboardFocus.OnDemand : WlrKeyboardFocus.Exclusive)
        onBackingWindowVisibleChanged: if (backingWindowVisible) focusTimer.restart()

        Rectangle {
            anchors.fill: parent
            radius: Commons.Style.cornerRadius
            color: Commons.Color.popups.background
            border.color: Commons.Color.accent
            border.width: 1
            ScrollView {
                id: scroll
                anchors.fill: parent
                anchors.margins: 24
                clip: true
                contentWidth: availableWidth
                ScrollBar.horizontal.policy: ScrollBar.AlwaysOff
                ColumnLayout {
                    id: content
                    width: scroll.availableWidth
                    spacing: 14
                    Keys.onEscapePressed: root.reply(true)
                    Label {
                        text: "omaquery"
                        font.family: Commons.Style.font.menuFamily
                        font.pixelSize: 22
                        font.bold: true
                        color: Commons.Color.foreground
                    }
                    Label {
                        text: "A command is requesting environment values"
                        color: Commons.Color.muted
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                    Label {
                        visible: !!root.request && !!root.request.origin
                        text: root.request && root.request.origin ? "Remote request from " + root.request.origin : ""
                        textFormat: Text.PlainText
                        color: Commons.Color.accent
                        wrapMode: Text.WrapAnywhere
                        Layout.fillWidth: true
                    }
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: details.implicitHeight + 24
                        radius: 8
                        color: Commons.Color.background
                        border.color: Commons.Color.muted
                        ColumnLayout {
                            id: details
                            anchors.fill: parent
                            anchors.margins: 12
                            spacing: 8
                            Label {
                                text: root.request ? root.request.cwd : ""
                                textFormat: Text.PlainText
                                color: Commons.Color.muted
                                wrapMode: Text.WrapAnywhere
                                Layout.fillWidth: true
                            }
                            Label {
                                // JSON quoting makes argument boundaries visible,
                                // including whitespace, newlines, and shell syntax.
                                text: root.request ? root.request.command.map(a => JSON.stringify(a)).join(" ") : ""
                                textFormat: Text.PlainText
                                font.family: "monospace"
                                color: Commons.Color.foreground
                                wrapMode: Text.WrapAnywhere
                                Layout.fillWidth: true
                            }
                        }
                    }
                    Label {
                        visible: text.length > 0
                        text: root.request ? root.request.reason || "" : ""
                        textFormat: Text.PlainText
                        color: Commons.Color.foreground
                        wrapMode: Text.Wrap
                        Layout.fillWidth: true
                    }
                    Repeater {
                        id: fields
                        model: root.request ? root.request.fields : []
                        delegate: ColumnLayout {
                            id: fieldRow
                            required property var modelData
                            required property int index
                            property alias value: input.text
                            Layout.fillWidth: true
                            spacing: 6
                            function clear() { input.text = ""; reveal.checked = false }
                            function focusInput() { input.forceActiveFocus() }
                            Label {
                                text: fieldRow.modelData.name
                                textFormat: Text.PlainText
                                font.family: "monospace"
                                font.bold: true
                                color: Commons.Color.foreground
                            }
                            Label {
                                visible: text.length > 0
                                text: fieldRow.modelData.hint || ""
                                textFormat: Text.PlainText
                                color: Commons.Color.muted
                                wrapMode: Text.Wrap
                                Layout.fillWidth: true
                            }
                            RowLayout {
                                Layout.fillWidth: true
                                TextField {
                                    id: input
                                    Layout.fillWidth: true
                                    enabled: !root.submitting
                                    echoMode: reveal.checked ? TextInput.Normal : TextInput.Password
                                    passwordMaskDelay: 0
                                    selectByMouse: true
                                    placeholderText: "Enter or paste value"
                                    color: Commons.Color.foreground
                                    placeholderTextColor: Commons.Color.muted
                                    selectionColor: Commons.Color.accent
                                    selectedTextColor: Commons.Color.background
                                    inputMethodHints: Qt.ImhHiddenText | Qt.ImhSensitiveData | Qt.ImhNoPredictiveText
                                    padding: 12
                                    background: Rectangle {
                                        radius: 6
                                        color: Commons.Color.background
                                        border.width: input.activeFocus ? 2 : 1
                                        border.color: input.activeFocus ? Commons.Color.accent : Commons.Color.muted
                                    }
                                    onAccepted: {
                                        if (fieldRow.index + 1 < fields.count) fields.itemAt(fieldRow.index + 1).focusInput()
                                        else root.reply(false)
                                    }
                                    Keys.onEscapePressed: root.reply(true)
                                }
                                CheckBox {
                                    id: reveal
                                    text: "Show"
                                    enabled: !root.submitting
                                    palette.windowText: Commons.Color.foreground
                                    Keys.onEscapePressed: root.reply(true)
                                }
                            }
                        }
                    }
                    Label {
                        Layout.fillWidth: true
                        text: "Values go to this command. Its output may expose them."
                        color: Commons.Color.muted
                        wrapMode: Text.Wrap
                        font.pixelSize: 12
                    }
                    RowLayout {
                        Layout.fillWidth: true
                        Button {
                            text: "Cancel · Esc"
                            enabled: !root.submitting
                            onClicked: root.reply(true)
                            Keys.onEscapePressed: root.reply(true)
                        }
                        Item { Layout.fillWidth: true }
                        Button {
                            text: root.submitting ? "Submitting…" : "Run command"
                            enabled: !root.submitting
                            highlighted: true
                            palette.highlight: Commons.Color.accent
                            palette.highlightedText: Commons.Color.background
                            onClicked: root.reply(false)
                            Keys.onEscapePressed: root.reply(true)
                        }
                    }
                }
            }
        }
    }
}
