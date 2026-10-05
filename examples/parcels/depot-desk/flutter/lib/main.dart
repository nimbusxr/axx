// The depot desk in Flutter (../../README.md).
import 'dart:convert';
import 'dart:io';
import 'dart:ui' show PointMode, SemanticsRole;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

const towns = ['Leipzig', 'Halle', 'Markkleeberg', 'Taucha', 'Schkeuditz', 'Delitzsch', 'Borna',
    'Grimma', 'Wurzen', 'Eilenburg'];
final expected = [for (var i = 0; i < 40; i++) ('PX-DSK-${4101 + i}', towns[i % towns.length])];

String parcels(int n) => n == 1 ? '1 parcel' : '$n parcels';

class Arrival {
  Arrival(this.reference, this.level, this.fragile);
  final String reference;
  final String level;
  final bool fragile;
  Map<String, Object> toJson() => {'reference': reference, 'level': level, 'fragile': fragile};
  static Arrival fromJson(Map<String, dynamic> j) => Arrival(j['reference'], j['level'], j['fragile']);
}

/// The day's arrivals, in arrivals.json in the desk's data folder.
class Store {
  static Future<File> file() async => File('${(await getApplicationSupportDirectory()).path}/arrivals.json');
  static Future<List<Arrival>> load() async {
    try {
      final list = jsonDecode(await (await file()).readAsString()) as List;
      return [for (final j in list) Arrival.fromJson(j)];
    } catch (_) {
      return [];
    }
  }

  static Future<void> save(List<Arrival> arrivals) async {
    final f = await file();
    await f.parent.create(recursive: true);
    await f.writeAsString(jsonEncode([for (final a in arrivals) a.toJson()]));
  }
}

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final prefs = await SharedPreferences.getInstance();
  final arrivals = await Store.load();
  runApp(MaterialApp(
    title: 'Depot desk',
    debugShowCheckedModeBanner: false,
    home: Desk(prefs: prefs, arrivals: arrivals),
  ));
}

class Desk extends StatefulWidget {
  const Desk({super.key, required this.prefs, required this.arrivals});
  final SharedPreferences prefs;
  final List<Arrival> arrivals;

  @override
  State<Desk> createState() => _DeskState();
}

class _DeskState extends State<Desk> {
  final reference = TextEditingController();
  late List<Arrival> arrivals = widget.arrivals;
  late String level = widget.prefs.getString('serviceLevel') ?? 'Standard';
  bool fragile = false;
  bool printLabel = false;
  late String status = arrivals.isEmpty ? 'No parcels registered yet' : '${parcels(arrivals.length)} registered today';
  final strokes = <List<Offset>>[];
  bool showRules = false;

  @override
  void initState() {
    super.initState();
    reference.addListener(() => setState(() {}));
  }

  Future<void> register() async {
    final ref = reference.text.trim();
    if (ref.isEmpty) return;
    if (arrivals.any((a) => a.reference == ref)) {
      setState(() => status = '$ref is already registered');
      return;
    }
    arrivals = [...arrivals, Arrival(ref, level, fragile)];
    await Store.save(arrivals);
    await widget.prefs.setString('serviceLevel', level);
    setState(() {
      status = 'Registered $ref: $level${fragile ? ', fragile' : ''}${printLabel ? ', label printed' : ''}';
      reference.clear();
      fragile = false;
    });
  }

  Future<void> closeDay() async {
    final n = arrivals.length;
    arrivals = [];
    await Store.save(arrivals);
    setState(() => status = 'Day closed: ${parcels(n)} handed over');
  }

  @override
  Widget build(BuildContext context) {
    final closeDayAction = arrivals.isEmpty ? null : closeDay;
    final body = DefaultTabController(
      length: 2,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          if (!Platform.isMacOS)
            MenuBar(children: [
              SubmenuButton(menuChildren: [
                MenuItemButton(onPressed: closeDayAction, child: const Text('Close day')),
              ], child: const Text('Depot')),
            ]),
          Image.asset('assets/depot.png', width: 64, height: 64, semanticLabel: 'Leipzig depot'),
          const TabBar(isScrollable: true, tabAlignment: TabAlignment.start, tabs: [Tab(text: 'Arrivals'), Tab(text: 'Handover')]),
          Expanded(child: TabBarView(children: [arrivalsTab(), handoverTab()])),
        ]),
      ),
    );
    return Scaffold(
      body: Platform.isMacOS
          ? PlatformMenuBar(menus: [
              const PlatformMenu(label: 'Depot desk', menus: [
                PlatformProvidedMenuItem(type: PlatformProvidedMenuItemType.quit),
              ]),
              PlatformMenu(label: 'Depot', menus: [
                PlatformMenuItem(label: 'Close day', onSelected: closeDayAction),
              ]),
            ], child: body)
          : body,
    );
  }

  // One column, taller than the window: it scrolls, as a page does.
  Widget arrivalsTab() {
    return SingleChildScrollView(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [form(), lists()]),
    );
  }

  Widget form() {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      CallbackShortcuts(
        bindings: {const SingleActivator(LogicalKeyboardKey.escape): reference.clear},
        child: TextField(
          controller: reference,
          decoration: const InputDecoration(labelText: 'Reference'),
          onSubmitted: (_) => register(),
        ),
      ),
      CheckboxListTile(
        title: const Text('Fragile'),
        value: fragile,
        controlAffinity: ListTileControlAffinity.leading,
        onChanged: (v) => setState(() => fragile = v ?? false),
      ),
      RadioGroup<String>(
        groupValue: level,
        onChanged: (v) => setState(() => level = v ?? level),
        child: const Row(children: [
          Expanded(child: RadioListTile<String>(title: Text('Standard'), value: 'Standard')),
          Expanded(child: RadioListTile<String>(title: Text('Express'), value: 'Express')),
        ]),
      ),
      SwitchListTile(
        title: const Text('Print label'),
        value: printLabel,
        onChanged: (v) => setState(() => printLabel = v),
      ),
      ElevatedButton(onPressed: reference.text.trim().isEmpty ? null : register, child: const Text('Register')),
      Padding(padding: const EdgeInsets.symmetric(vertical: 8), child: Text(status)),
    ]);
  }

  Widget lists() {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      const Text('Arrivals'),
      Semantics(
        label: 'Arrivals',
        role: SemanticsRole.list,
        child: SizedBox(
          height: 160,
          child: ListView(children: [
            for (final a in arrivals)
              Semantics(
                role: SemanticsRole.listItem,
                child: ListTile(
                  dense: true,
                  title: Text(a.reference),
                  onTap: () => setState(() => status = '${a.reference}: ${a.level}${a.fragile ? ', fragile' : ''}'),
                ),
              ),
          ]),
        ),
      ),
      const Text('Expected today'),
      SizedBox(
        height: 8 * 32 + 40,
        child: SingleChildScrollView(
          child: DataTable(
            headingRowHeight: 40,
            dataRowMinHeight: 32,
            dataRowMaxHeight: 32,
            showCheckboxColumn: false,
            columns: const [DataColumn(label: Text('Reference')), DataColumn(label: Text('Town'))],
            rows: [
              for (final (ref, town) in expected)
                DataRow(
                  onSelectChanged: (_) => setState(() => reference.text = ref),
                  cells: [DataCell(Text(ref)), DataCell(Text(town))],
                ),
            ],
          ),
        ),
      ),
    ]);
  }

  Widget handoverTab() {
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      // Where the courier signs: a click puts a dot, a drag draws a stroke.
      Semantics(
        label: 'Courier signature',
        image: true,
        child: GestureDetector(
          onPanDown: (d) => setState(() => strokes.add([d.localPosition])),
          onPanUpdate: (d) => setState(() => strokes.last.add(d.localPosition)),
          child: CustomPaint(size: const Size(400, 160), painter: InkPainter(strokes)),
        ),
      ),
      Text(strokes.isEmpty ? 'Not signed' : 'Signed'),
      TextButton(onPressed: () => setState(strokes.clear), child: const Text('Clear signature')),
      Semantics(
        link: true,
        child: InkWell(
          onTap: () => setState(() => showRules = true),
          child: const Text('Handover rules', style: TextStyle(color: Colors.blue, decoration: TextDecoration.underline)),
        ),
      ),
      if (showRules) const Text('Parcels are handed over to the courier at 18:00.'),
    ]);
  }
}

class InkPainter extends CustomPainter {
  InkPainter(this.strokes);
  final List<List<Offset>> strokes;

  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawRect(Offset.zero & size, Paint()..color = Colors.white);
    final pen = Paint()
      ..color = Colors.black
      ..strokeWidth = 3
      ..strokeCap = StrokeCap.round;
    for (final s in strokes) {
      if (s.length == 1) canvas.drawPoints(PointMode.points, s, pen);
      for (var i = 1; i < s.length; i++) {
        canvas.drawLine(s[i - 1], s[i], pen);
      }
    }
  }

  @override
  bool shouldRepaint(InkPainter old) => true;
}
