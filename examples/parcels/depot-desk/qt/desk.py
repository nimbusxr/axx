"""The depot desk in Qt: PyQt6 by default, PyQt5 with the argument --qt5."""

import json
import os
import sys

if "--qt5" in sys.argv:
    from PyQt5.QtCore import QSettings, QStandardPaths, Qt
    from PyQt5.QtGui import QKeySequence, QPainter, QPen, QPixmap
    from PyQt5.QtWidgets import (QAbstractItemView, QAction, QApplication, QButtonGroup,
                                 QCheckBox, QHBoxLayout, QLabel, QLineEdit, QListWidget,
                                 QMainWindow, QPushButton, QRadioButton, QScrollArea,
                                 QShortcut, QTableWidget, QTableWidgetItem, QTabWidget,
                                 QVBoxLayout, QWidget)
else:
    from PyQt6.QtCore import QSettings, QStandardPaths, Qt
    from PyQt6.QtGui import QAction, QKeySequence, QPainter, QPen, QPixmap, QShortcut
    from PyQt6.QtWidgets import (QAbstractItemView, QApplication, QButtonGroup, QCheckBox,
                                 QHBoxLayout, QLabel, QLineEdit, QListWidget, QMainWindow,
                                 QPushButton, QRadioButton, QScrollArea, QTableWidget,
                                 QTableWidgetItem, QTabWidget, QVBoxLayout, QWidget)

HERE = os.path.dirname(os.path.abspath(__file__))
TOWNS = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna",
         "Grimma", "Wurzen", "Eilenburg"]
EXPECTED = [("PX-DSK-%d" % (4101 + i), TOWNS[i % len(TOWNS)]) for i in range(40)]


def parcels(n):
    return "1 parcel" if n == 1 else "%d parcels" % n


class Store:
    """The day's arrivals, in arrivals.json in the desk's data folder."""

    def __init__(self):
        folder = QStandardPaths.writableLocation(QStandardPaths.StandardLocation.AppDataLocation)
        os.makedirs(folder, exist_ok=True)
        self.path = os.path.join(folder, "arrivals.json")

    def load(self):
        try:
            with open(self.path) as f:
                return json.load(f)
        except FileNotFoundError:
            return []

    def save(self, arrivals):
        with open(self.path, "w") as f:
            json.dump(arrivals, f)


class SignaturePad(QLabel):
    """Where the courier signs: a click puts a dot, a drag draws a stroke.

    A label showing the drawing, so that accessibility has it as an image: a
    plain widget has no role, and macOS leaves it out (PyQt cannot give one).
    """

    def __init__(self, changed):
        super().__init__()
        self.setFixedSize(400, 160)
        self.setAccessibleName("Courier signature")
        self.changed = changed
        self.clear()

    def point(self, event):
        return event.position().toPoint() if hasattr(event, "position") else event.pos()

    def mousePressEvent(self, event):
        self.last = self.point(event)
        self.draw(self.last, self.last)
        self.changed()

    def mouseMoveEvent(self, event):
        at = self.point(event)
        self.draw(self.last, at)
        self.last = at

    def draw(self, a, b):
        painter = QPainter(self.ink)
        painter.setRenderHint(QPainter.RenderHint.Antialiasing)
        painter.setPen(QPen(Qt.GlobalColor.black, 3, Qt.PenStyle.SolidLine, Qt.PenCapStyle.RoundCap))
        painter.drawLine(a, b)
        painter.end()
        self.setPixmap(self.ink)

    def clear(self):
        self.ink = QPixmap(400, 160)
        self.ink.fill(Qt.GlobalColor.white)
        self.setPixmap(self.ink)


class Desk(QMainWindow):
    def __init__(self):
        super().__init__()
        self.setWindowTitle("Depot desk")
        self.store = Store()
        self.settings = QSettings()
        self.arrivals = self.store.load()

        self.close_day = QAction("Close day", self)
        self.close_day.triggered.connect(self.on_close_day)
        self.menuBar().addMenu("Depot").addAction(self.close_day)

        logo = QLabel()
        logo.setPixmap(QPixmap(os.path.join(HERE, "..", "depot.png")))
        logo.setAccessibleName("Leipzig depot")

        tabs = QTabWidget()
        tabs.addTab(self.arrivals_tab(), "Arrivals")
        tabs.addTab(self.handover_tab(), "Handover")

        root = QWidget()
        layout = QVBoxLayout(root)
        layout.addWidget(logo)
        layout.addWidget(tabs)
        self.setCentralWidget(root)
        self.resize(640, 580)
        self.refresh()
        self.status.setText(("%s registered today" % parcels(len(self.arrivals)))
                            if self.arrivals else "No parcels registered yet")

    def arrivals_tab(self):
        # One column, taller than the window: it scrolls.
        tab = QWidget()
        layout = QVBoxLayout(tab)
        lists = layout

        label = QLabel("Reference")
        self.reference = QLineEdit()
        self.reference.setAccessibleName("Reference")
        label.setBuddy(self.reference)
        self.reference.textChanged.connect(self.refresh)
        self.reference.returnPressed.connect(self.register)
        escape = QShortcut(QKeySequence("Escape"), self.reference)
        escape.setContext(Qt.ShortcutContext.WidgetShortcut)
        escape.activated.connect(self.reference.clear)
        layout.addWidget(label)
        layout.addWidget(self.reference)

        self.fragile = QCheckBox("Fragile")
        layout.addWidget(self.fragile)

        levels = QHBoxLayout()
        self.standard = QRadioButton("Standard")
        self.express = QRadioButton("Express")
        group = QButtonGroup(tab)
        for b in (self.standard, self.express):
            group.addButton(b)
            levels.addWidget(b)
        levels.addStretch()
        (self.express if self.settings.value("serviceLevel") == "Express" else self.standard).setChecked(True)
        layout.addLayout(levels)

        self.print_label = QCheckBox("Print label")  # Qt has no switch
        layout.addWidget(self.print_label)

        self.register_button = QPushButton("Register")
        self.register_button.clicked.connect(self.register)
        layout.addWidget(self.register_button)

        self.status = QLabel()
        layout.addWidget(self.status)

        lists.addWidget(QLabel("Arrivals"))
        self.list = QListWidget()
        self.list.setAccessibleName("Arrivals")
        self.list.setFixedHeight(160)
        self.list.itemClicked.connect(self.on_arrival)
        lists.addWidget(self.list)

        lists.addWidget(QLabel("Expected today"))
        self.expected = QTableWidget(len(EXPECTED), 2)
        self.expected.setAccessibleName("Expected today")
        self.expected.setHorizontalHeaderLabels(["Reference", "Town"])
        self.expected.verticalHeader().setVisible(False)
        self.expected.setSelectionBehavior(QAbstractItemView.SelectionBehavior.SelectRows)
        self.expected.setEditTriggers(QAbstractItemView.EditTrigger.NoEditTriggers)
        for row, (ref, town) in enumerate(EXPECTED):
            self.expected.setItem(row, 0, QTableWidgetItem(ref))
            self.expected.setItem(row, 1, QTableWidgetItem(town))
        self.expected.setFixedHeight(8 * self.expected.rowHeight(0) + self.expected.horizontalHeader().height() + 4)
        self.expected.cellClicked.connect(lambda row, _: self.reference.setText(EXPECTED[row][0]))
        lists.addWidget(self.expected)
        page = QScrollArea()
        page.setWidget(tab)
        page.setWidgetResizable(True)
        return page

    def handover_tab(self):
        tab = QWidget()
        layout = QVBoxLayout(tab)
        self.signed = QLabel("Not signed")
        self.pad = SignaturePad(lambda: self.signed.setText("Signed"))
        layout.addWidget(self.pad)
        layout.addWidget(self.signed)
        clear = QPushButton("Clear signature")
        clear.clicked.connect(self.clear_signature)
        layout.addWidget(clear)
        # Qt has no link: a label's link, which accessibility reads as text.
        rules = QLabel('<a href="#rules">Handover rules</a>')
        rules.setTextInteractionFlags(Qt.TextInteractionFlag.LinksAccessibleByMouse | Qt.TextInteractionFlag.LinksAccessibleByKeyboard)
        self.rules_text = QLabel("Parcels are handed over to the courier at 18:00.")
        self.rules_text.hide()
        rules.linkActivated.connect(lambda _: self.rules_text.show())
        layout.addWidget(rules, 0, Qt.AlignmentFlag.AlignLeft)
        layout.addWidget(self.rules_text)
        layout.addStretch()
        return tab

    def refresh(self):
        self.register_button.setEnabled(bool(self.reference.text().strip()))
        self.close_day.setEnabled(bool(self.arrivals))
        self.list.clear()
        for a in self.arrivals:
            self.list.addItem(a["reference"])

    def register(self):
        ref = self.reference.text().strip()
        if not ref:
            return
        if any(a["reference"] == ref for a in self.arrivals):
            self.status.setText("%s is already registered" % ref)
            return
        level = "Express" if self.express.isChecked() else "Standard"
        fragile = self.fragile.isChecked()
        self.arrivals.append({"reference": ref, "level": level, "fragile": fragile})
        self.store.save(self.arrivals)
        self.settings.setValue("serviceLevel", level)
        self.settings.sync()
        self.status.setText("Registered %s: %s%s%s" % (ref, level, ", fragile" if fragile else "",
                                                      ", label printed" if self.print_label.isChecked() else ""))
        self.reference.clear()
        self.fragile.setChecked(False)
        self.refresh()

    def on_arrival(self, item):
        a = next(a for a in self.arrivals if a["reference"] == item.text())
        self.status.setText("%s: %s%s" % (a["reference"], a["level"], ", fragile" if a["fragile"] else ""))

    def on_close_day(self):
        n = len(self.arrivals)
        self.arrivals = []
        self.store.save(self.arrivals)
        self.status.setText("Day closed: %s handed over" % parcels(n))
        self.refresh()

    def clear_signature(self):
        self.pad.clear()
        self.signed.setText("Not signed")


def main():
    app = QApplication([a for a in sys.argv if a != "--qt5"])
    app.setOrganizationName("Parcels")
    app.setOrganizationDomain("parcels.example")
    app.setApplicationName("Depot desk")
    desk = Desk()
    desk.show()
    sys.exit(app.exec() if hasattr(app, "exec") else app.exec_())


if __name__ == "__main__":
    main()
