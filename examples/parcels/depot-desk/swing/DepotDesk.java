// The depot desk in Swing (../README.md). build.sh makes it a jar.
import java.awt.BasicStroke;
import java.awt.BorderLayout;
import java.awt.Color;
import java.awt.Component;
import java.awt.Cursor;
import java.awt.Dimension;
import java.awt.FlowLayout;
import java.awt.Graphics;
import java.awt.Graphics2D;
import java.awt.Point;
import java.awt.Rectangle;
import java.awt.RenderingHints;
import java.awt.event.KeyEvent;
import java.awt.event.MouseAdapter;
import java.awt.event.MouseEvent;
import java.io.IOException;
import java.io.Reader;
import java.io.Writer;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Properties;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import javax.accessibility.AccessibleContext;
import javax.accessibility.AccessibleRole;
import javax.swing.AbstractAction;
import javax.swing.BorderFactory;
import javax.swing.Box;
import javax.swing.BoxLayout;
import javax.swing.ButtonGroup;
import javax.swing.DefaultListModel;
import javax.swing.Icon;
import javax.swing.ImageIcon;
import javax.swing.JButton;
import javax.swing.JCheckBox;
import javax.swing.JComponent;
import javax.swing.JFrame;
import javax.swing.JLabel;
import javax.swing.JList;
import javax.swing.JMenu;
import javax.swing.JMenuBar;
import javax.swing.JMenuItem;
import javax.swing.JPanel;
import javax.swing.JRadioButton;
import javax.swing.JScrollPane;
import javax.swing.JTabbedPane;
import javax.swing.JTable;
import javax.swing.JTextField;
import javax.swing.KeyStroke;
import javax.swing.ListSelectionModel;
import javax.swing.Scrollable;
import javax.swing.SwingUtilities;
import javax.swing.event.DocumentEvent;
import javax.swing.event.DocumentListener;
import javax.swing.table.DefaultTableModel;

public class DepotDesk {
    static final String[] TOWNS = {"Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz",
        "Delitzsch", "Borna", "Grimma", "Wurzen", "Eilenburg"};

    record Arrival(String reference, String level, boolean fragile) {}

    static String parcels(int n) {
        return n == 1 ? "1 parcel" : n + " parcels";
    }

    /** The desk's folder: the day's arrivals, and its settings in a file (see README.md). */
    static final Path FOLDER = Path.of(System.getProperty("user.home"), ".depot-desk");

    static List<Arrival> load() {
        List<Arrival> out = new ArrayList<>();
        try {
            String json = Files.readString(FOLDER.resolve("arrivals.json"));
            Matcher m = Pattern.compile("\\{\"reference\":\"([^\"]*)\",\"level\":\"([^\"]*)\",\"fragile\":(true|false)\\}").matcher(json);
            while (m.find()) {
                out.add(new Arrival(m.group(1), m.group(2), Boolean.parseBoolean(m.group(3))));
            }
        } catch (IOException e) {
            // no arrivals yet
        }
        return out;
    }

    static void save(List<Arrival> arrivals) {
        StringBuilder json = new StringBuilder("[");
        for (Arrival a : arrivals) {
            if (json.length() > 1) json.append(",");
            json.append(String.format("{\"reference\":\"%s\",\"level\":\"%s\",\"fragile\":%s}", a.reference(), a.level(), a.fragile()));
        }
        try {
            Files.createDirectories(FOLDER);
            Files.writeString(FOLDER.resolve("arrivals.json"), json.append("]").toString());
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }

    static Properties settings() {
        Properties p = new Properties();
        try (Reader r = Files.newBufferedReader(FOLDER.resolve("settings.properties"))) {
            p.load(r);
        } catch (IOException e) {
            // no settings yet
        }
        return p;
    }

    static void saveSettings(Properties p) {
        try {
            Files.createDirectories(FOLDER);
            try (Writer w = Files.newBufferedWriter(FOLDER.resolve("settings.properties"))) {
                p.store(w, "Depot desk");
            }
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }

    /**
     * A picture: a label showing an image, with the icon role, as Java gives a label the label
     * role whatever it shows (Linux reads it as text, macOS as an image).
     */
    static class Picture extends JLabel {
        Picture(Icon icon, String name) {
            super(icon);
            getAccessibleContext().setAccessibleName(name);
        }

        @Override public AccessibleContext getAccessibleContext() {
            if (accessibleContext == null) {
                accessibleContext = new AccessibleJLabel() {
                    @Override public AccessibleRole getAccessibleRole() { return AccessibleRole.ICON; }
                };
            }
            return accessibleContext;
        }
    }

    /**
     * Where the courier signs: a click puts a dot, a drag draws a stroke. A picture of the
     * drawing: macOS leaves a component that draws itself out of the tree, whatever its role.
     */
    static class SignaturePad extends Picture {
        final List<List<Point>> strokes = new ArrayList<>();
        Runnable changed = () -> {};

        SignaturePad() {
            super(null, "Courier signature");
            setIcon(new Icon() {
                @Override public int getIconWidth() { return 400; }
                @Override public int getIconHeight() { return 160; }
                @Override public void paintIcon(Component c, Graphics g, int x, int y) {
                    Graphics2D g2 = (Graphics2D) g.create();
                    g2.translate(x, y);
                    g2.setColor(Color.WHITE);
                    g2.fillRect(0, 0, 400, 160);
                    g2.setColor(Color.BLACK);
                    g2.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
                    g2.setStroke(new BasicStroke(3, BasicStroke.CAP_ROUND, BasicStroke.JOIN_ROUND));
                    for (List<Point> s : strokes) {
                        Point last = s.get(0);
                        for (Point p : s) {
                            g2.drawLine(last.x, last.y, p.x, p.y);
                            last = p;
                        }
                    }
                    g2.dispose();
                }
            });
            setMaximumSize(new Dimension(400, 160));
            MouseAdapter mouse = new MouseAdapter() {
                @Override public void mousePressed(MouseEvent e) {
                    List<Point> stroke = new ArrayList<>();
                    stroke.add(e.getPoint());
                    strokes.add(stroke);
                    repaint();
                    changed.run();
                }
                @Override public void mouseDragged(MouseEvent e) {
                    strokes.get(strokes.size() - 1).add(e.getPoint());
                    repaint();
                }
            };
            addMouseListener(mouse);
            addMouseMotionListener(mouse);
        }
    }

    /** Swing has no link: a label people click, with the hyperlink role for accessibility. */
    static class Link extends JLabel {
        Link(String text, Runnable action) {
            super("<html><a href=''>" + text + "</a></html>");
            setCursor(Cursor.getPredefinedCursor(Cursor.HAND_CURSOR));
            addMouseListener(new MouseAdapter() {
                @Override public void mouseClicked(MouseEvent e) { action.run(); }
            });
            getAccessibleContext().setAccessibleName(text);
        }

        @Override public AccessibleContext getAccessibleContext() {
            if (accessibleContext == null) {
                accessibleContext = new AccessibleJLabel() {
                    @Override public AccessibleRole getAccessibleRole() { return AccessibleRole.HYPERLINK; }
                };
            }
            return accessibleContext;
        }
    }

    final List<Arrival> arrivals = load();
    final Properties settings = settings();
    final JFrame frame = new JFrame("Depot desk");
    final JTextField reference = new JTextField();
    final JCheckBox fragile = new JCheckBox("Fragile");
    final JRadioButton standard = new JRadioButton("Standard");
    final JRadioButton express = new JRadioButton("Express");
    final JCheckBox printLabel = new JCheckBox("Print label"); // Swing has no switch
    final JButton register = new JButton("Register");
    final JLabel status = new JLabel();
    final DefaultListModel<String> arrivalsModel = new DefaultListModel<>();
    final JMenuItem closeDay = new JMenuItem("Close day");
    final SignaturePad pad = new SignaturePad();
    final JLabel signed = new JLabel("Not signed");
    final JLabel rulesText = new JLabel("Parcels are handed over to the courier at 18:00.");

    void build() {
        JMenu depot = new JMenu("Depot");
        closeDay.addActionListener(e -> closeDay());
        depot.add(closeDay);
        JMenuBar bar = new JMenuBar();
        bar.add(depot);
        frame.setJMenuBar(bar);

        JLabel logo = new Picture(new ImageIcon(DepotDesk.class.getResource("/depot.png")), "Leipzig depot");
        JTabbedPane tabs = new JTabbedPane();
        tabs.addTab("Arrivals", arrivalsTab());
        tabs.addTab("Handover", handoverTab());

        JPanel root = new JPanel(new BorderLayout());
        root.setBorder(BorderFactory.createEmptyBorder(12, 12, 12, 12));
        JPanel top = new JPanel(new FlowLayout(FlowLayout.LEFT));
        top.add(logo);
        root.add(top, BorderLayout.NORTH);
        root.add(tabs, BorderLayout.CENTER);
        frame.setContentPane(root);
        frame.setDefaultCloseOperation(JFrame.EXIT_ON_CLOSE);
        frame.setSize(640, 580);
        refresh();
        status.setText(arrivals.isEmpty() ? "No parcels registered yet" : parcels(arrivals.size()) + " registered today");
        frame.setVisible(true);
    }

    /** A column as wide as the page it scrolls in. */
    static final class Column extends JPanel implements Scrollable {
        @Override public Dimension getPreferredScrollableViewportSize() { return getPreferredSize(); }
        @Override public int getScrollableUnitIncrement(Rectangle visible, int orientation, int direction) { return 16; }
        @Override public int getScrollableBlockIncrement(Rectangle visible, int orientation, int direction) { return visible.height; }
        @Override public boolean getScrollableTracksViewportWidth() { return true; }
        @Override public boolean getScrollableTracksViewportHeight() { return false; }
    }

    JComponent arrivalsTab() {
        // One column, taller than the window: it scrolls, as a page does.
        Column p = new Column();
        p.setLayout(new BoxLayout(p, BoxLayout.Y_AXIS));
        JLabel label = new JLabel("Reference");
        label.setLabelFor(reference);
        reference.getAccessibleContext().setAccessibleName("Reference");
        reference.setMaximumSize(new Dimension(Integer.MAX_VALUE, reference.getPreferredSize().height));
        reference.addActionListener(e -> register());
        reference.getInputMap().put(KeyStroke.getKeyStroke(KeyEvent.VK_ESCAPE, 0), "clear");
        reference.getActionMap().put("clear", new AbstractAction() {
            @Override public void actionPerformed(java.awt.event.ActionEvent e) { reference.setText(""); }
        });
        reference.getDocument().addDocumentListener(new DocumentListener() {
            @Override public void insertUpdate(DocumentEvent e) { refresh(); }
            @Override public void removeUpdate(DocumentEvent e) { refresh(); }
            @Override public void changedUpdate(DocumentEvent e) { refresh(); }
        });
        ButtonGroup levels = new ButtonGroup();
        levels.add(standard);
        levels.add(express);
        ("Express".equals(settings.getProperty("serviceLevel")) ? express : standard).setSelected(true);
        JPanel levelRow = new JPanel(new FlowLayout(FlowLayout.LEFT, 0, 0));
        levelRow.add(standard);
        levelRow.add(express);
        register.addActionListener(e -> register());

        JList<String> list = new JList<>(arrivalsModel);
        list.getAccessibleContext().setAccessibleName("Arrivals");
        list.setSelectionMode(ListSelectionModel.SINGLE_SELECTION);
        list.addListSelectionListener(e -> {
            int i = list.getSelectedIndex();
            if (e.getValueIsAdjusting() || i < 0) return;
            Arrival a = arrivals.get(i);
            status.setText(a.reference() + ": " + a.level() + (a.fragile() ? ", fragile" : ""));
            SwingUtilities.invokeLater(list::clearSelection);
        });
        JScrollPane listScroll = new JScrollPane(list);
        listScroll.setPreferredSize(new Dimension(380, 160));
        listScroll.setMaximumSize(new Dimension(Integer.MAX_VALUE, 160));

        DefaultTableModel model = new DefaultTableModel(new Object[] {"Reference", "Town"}, 0) {
            @Override public boolean isCellEditable(int row, int column) { return false; }
        };
        for (int i = 0; i < 40; i++) {
            model.addRow(new Object[] {"PX-DSK-" + (4101 + i), TOWNS[i % TOWNS.length]});
        }
        JTable table = new JTable(model);
        table.getAccessibleContext().setAccessibleName("Expected today");
        table.setSelectionMode(ListSelectionModel.SINGLE_SELECTION);
        table.getSelectionModel().addListSelectionListener(e -> {
            int i = table.getSelectedRow();
            if (e.getValueIsAdjusting() || i < 0) return;
            reference.setText((String) model.getValueAt(i, 0));
            SwingUtilities.invokeLater(table::clearSelection);
        });
        JScrollPane tableScroll = new JScrollPane(table);
        int tableHeight = 8 * table.getRowHeight() + table.getTableHeader().getPreferredSize().height + 4;
        tableScroll.setPreferredSize(new Dimension(380, tableHeight));
        tableScroll.setMaximumSize(new Dimension(Integer.MAX_VALUE, tableHeight));

        for (JComponent c : new JComponent[] {label, reference, fragile, levelRow, printLabel, register, status,
                new JLabel("Arrivals"), listScroll, new JLabel("Expected today"), tableScroll}) {
            c.setAlignmentX(JComponent.LEFT_ALIGNMENT);
            p.add(c);
        }
        p.setBorder(BorderFactory.createEmptyBorder(8, 8, 8, 8));
        JScrollPane page = new JScrollPane(p, JScrollPane.VERTICAL_SCROLLBAR_AS_NEEDED, JScrollPane.HORIZONTAL_SCROLLBAR_NEVER);
        page.setBorder(null);
        return page;
    }

    JPanel handoverTab() {
        JPanel p = new JPanel();
        p.setLayout(new BoxLayout(p, BoxLayout.Y_AXIS));
        pad.changed = () -> signed.setText("Signed");
        JButton clear = new JButton("Clear signature");
        clear.addActionListener(e -> {
            pad.strokes.clear();
            pad.repaint();
            signed.setText("Not signed");
        });
        Link rules = new Link("Handover rules", () -> rulesText.setVisible(true));
        rules.setMaximumSize(rules.getPreferredSize());
        rulesText.setVisible(false);
        for (JComponent c : new JComponent[] {pad, signed, clear, rules, rulesText}) {
            c.setAlignmentX(JComponent.LEFT_ALIGNMENT);
            p.add(c);
        }
        p.add(Box.createVerticalGlue());
        return p;
    }

    void refresh() {
        register.setEnabled(!reference.getText().isBlank());
        closeDay.setEnabled(!arrivals.isEmpty());
        arrivalsModel.clear();
        arrivals.forEach(a -> arrivalsModel.addElement(a.reference()));
    }

    void register() {
        String ref = reference.getText().strip();
        if (ref.isEmpty()) return;
        if (arrivals.stream().anyMatch(a -> a.reference().equals(ref))) {
            status.setText(ref + " is already registered");
            return;
        }
        String level = express.isSelected() ? "Express" : "Standard";
        arrivals.add(new Arrival(ref, level, fragile.isSelected()));
        save(arrivals);
        settings.setProperty("serviceLevel", level);
        saveSettings(settings);
        status.setText("Registered " + ref + ": " + level + (fragile.isSelected() ? ", fragile" : "")
            + (printLabel.isSelected() ? ", label printed" : ""));
        reference.setText("");
        fragile.setSelected(false);
        refresh();
    }

    void closeDay() {
        int n = arrivals.size();
        arrivals.clear();
        save(arrivals);
        status.setText("Day closed: " + parcels(n) + " handed over");
        refresh();
    }

    public static void main(String[] args) {
        SwingUtilities.invokeLater(() -> new DepotDesk().build());
    }
}
