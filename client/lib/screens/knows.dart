/// What the assistant knows, and what it can do.
///
/// Everything here is read-only, and none of it is news to the server --
/// it is all things that already decide how the assistant behaves, with
/// no way to look at them. A memory is believed indefinitely and
/// corrected only by accident. Half the tools are withheld from any given
/// request by a mechanism nothing displays. The events arrive on their
/// own and nothing has ever read them back.
///
/// Tables rather than cards. These are rows of the same shape, read to be
/// compared and scanned -- which one is stale, which one is never used --
/// and a column of cards makes comparison impossible.
///
/// This page lives inside the settings ListView, so nothing here may be
/// Expanded and nothing may scroll on its own: an unbounded height
/// collapses either to nothing at all.
library;

import 'package:flutter/material.dart';

import '../design/design.dart';
import '../state/app_state.dart';

/// KnowsSection : Which part is showing.
enum KnowsSection {
  memories('Memories'),
  events('Events'),
  tools('Abilities'),
  vocabulary('Vocabulary'),
  remarks('Unasked');

  const KnowsSection(this.title);
  final String title;
}

/// KnowsModule : The settings page that shows all of it.
class KnowsModule extends StatefulWidget {
  const KnowsModule({
    super.key,
    required this.state,
    this.where = const {},
    this.onWhere,
  });

  final AppState state;

  /// where : Which section is open and what it is filtered to, from the
  /// address. This page is read by narrowing it down and then going
  /// away to check something, so coming back to the top of an
  /// unfiltered list is coming back to the wrong place.
  final Map<String, String> where;

  /// onWhere : Told when either changes, so the address follows.
  final ValueChanged<Map<String, String>>? onWhere;

  @override
  State<KnowsModule> createState() => _KnowsModuleState();
}

class _KnowsModuleState extends State<KnowsModule> {
  late KnowsSection _section = _sectionFrom(widget.where['section']);
  late String _kind = widget.where['kind'] ?? '';
  late String _search = widget.where['find'] ?? '';
  late final _searchField = TextEditingController(text: _search);

  /// _sectionFrom : The section a name stands for.
  ///
  /// An unknown name is the first section rather than an error. A
  /// shared or hand-edited address should land somewhere usable.
  static KnowsSection _sectionFrom(String? name) {
    for (final s in KnowsSection.values) {
      if (s.name == name) return s;
    }
    return KnowsSection.memories;
  }

  /// _went : Puts where the page is looking into the address.
  ///
  /// The section is always named, so a shared address opens the same
  /// page. The other two are only there when they are set, so a plain
  /// look at the events is a plain address.
  void _went() => widget.onWhere?.call({
    'section': _section.name,
    'kind': _kind,
    'find': _search,
  });

  @override
  void dispose() {
    _searchField.dispose();
    super.dispose();
  }

  @override
  void initState() {
    super.initState();
    _load(_section);
  }

  /// didUpdateWidget : Follows the address when it moves without us --
  /// the back button, or a pasted link.
  @override
  void didUpdateWidget(KnowsModule old) {
    super.didUpdateWidget(old);
    final section = _sectionFrom(widget.where['section']);
    final kind = widget.where['kind'] ?? '';
    final find = widget.where['find'] ?? '';
    if (section == _section && kind == _kind && find == _search) return;

    setState(() {
      _section = section;
      _kind = kind;
      _search = find;
    });
    if (_searchField.text != find) _searchField.text = find;
    _load(_section);
  }

  /// _load : Reads one section, and only when it is opened.
  ///
  /// The events will grow for years. Loading all four to show one would
  /// get slower every week for no benefit.
  void _load(KnowsSection section) {
    switch (section) {
      case KnowsSection.memories:
        widget.state.loadMemories();
      case KnowsSection.events:
        widget.state.loadEvents(kind: _kind);
      case KnowsSection.tools:
        widget.state.loadAbilities();
      case KnowsSection.vocabulary:
        widget.state.loadVocabulary();
      case KnowsSection.remarks:
        widget.state.loadRemarks();
    }
  }

  void _show(KnowsSection section) {
    _searchField.clear();
    setState(() {
      _section = section;
      _search = '';
    });
    _went();
    _load(section);
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: [
            for (final s in KnowsSection.values)
              ChoiceChip(
                label: Text(s.title),
                selected: _section == s,
                onSelected: (_) => _show(s),
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.lg),
        AppTextField(
          controller: _searchField,
          label: 'Search',
          hint: 'filter these rows',
          onChanged: (v) {
            setState(() => _search = v.trim().toLowerCase());
            _went();
          },
        ),
        const SizedBox(height: AppSpacing.lg),
        AnimatedBuilder(
          animation: widget.state,
          builder: (context, _) {
            if (widget.state.busy) {
              return const Padding(
                padding: EdgeInsets.all(AppSpacing.xxl),
                child: Center(child: AppSpinner()),
              );
            }
            return switch (_section) {
              KnowsSection.memories => _Memories(
                state: widget.state,
                search: _search,
              ),
              KnowsSection.events => _Events(
                state: widget.state,
                search: _search,
                kind: _kind,
                // Remembered and put in the address. The reading is
                // left to the section, which is the only thing that
                // knows which stretch of time is showing: reloading
                // from here dropped it back to everything.
                onKind: (k) {
                  setState(() => _kind = k);
                  _went();
                },
              ),
              KnowsSection.tools => _Tools(
                state: widget.state,
                search: _search,
              ),
              KnowsSection.vocabulary => _Words(
                state: widget.state,
                search: _search,
              ),
              KnowsSection.remarks => _Unasked(
                state: widget.state,
                search: _search,
              ),
            };
          },
        ),
      ],
    );
  }
}

/// _Table : A table of rows of the same shape.
///
/// Scrolled sideways rather than squeezed: a column that has to wrap to
/// fit stops being comparable down the page, which is the only reason to
/// have a table.
class _Table extends StatelessWidget {
  const _Table({
    required this.summary,
    required this.columns,
    required this.rows,
    required this.empty,
    this.tall = false,
  });

  final String summary;
  final List<String> columns;
  final List<List<Widget>> rows;
  final Widget empty;

  /// tall : Whether a row may grow to fit what is in it.
  ///
  /// For the one table whose point is the detail. Everywhere else a
  /// row is a single line on purpose, so that rows of the same
  /// shape can be compared down the page.
  final bool tall;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (rows.isEmpty) return empty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.only(bottom: AppSpacing.sm),
          child: Text(summary, style: theme.textTheme.bodySmall),
        ),
        SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: DataTable(
            headingRowHeight: 38,
            dataRowMinHeight: 36,
            dataRowMaxHeight: tall ? double.infinity : 72,
            columnSpacing: AppSpacing.xl,
            columns: [
              for (final c in columns)
                DataColumn(label: Text(c, style: theme.textTheme.labelMedium)),
            ],
            rows: [
              for (final r in rows)
                DataRow(cells: [for (final c in r) DataCell(c)]),
            ],
          ),
        ),
      ],
    );
  }
}

/// _cell : A line of table text, cut off rather than wrapped.
Widget _cell(BuildContext context, String text, {double width = 240}) =>
    SizedBox(
      width: width,
      child: Text(
        text,
        overflow: TextOverflow.ellipsis,
        style: Theme.of(context).textTheme.bodyMedium,
      ),
    );

class _Memories extends StatelessWidget {
  const _Memories({required this.state, required this.search});

  final AppState state;
  final String search;

  @override
  Widget build(BuildContext context) {
    final m = state.memories;
    if (m == null) return const SizedBox.shrink();

    final items = m.items
        .where(
          (x) =>
              search.isEmpty ||
              x.subject.toLowerCase().contains(search) ||
              x.body.toLowerCase().contains(search),
        )
        .toList();

    return _Table(
      summary: [
        '${items.length} of ${m.items.length} shown',
        '${m.always} in every prompt',
        if (m.unused > 0) '${m.unused} never used',
      ].join(' · '),
      columns: const [
        'Subject',
        'Memory',
        'Tier',
        'Used',
        'Last used',
        'By meaning',
      ],
      empty: const AppEmptyState(
        icon: Icons.psychology_outlined,
        title: 'Nothing to show',
        body:
            'Memories are written during a conversation, when something '
            'is worth keeping.',
      ),
      rows: [
        for (final x in items)
          [
            _cell(context, x.subject, width: 200),
            _cell(context, x.body, width: 380),
            Text(x.tier),
            Text(x.uses == 0 ? '—' : '${x.uses}'),
            Text(x.lastUsedAt == null ? 'never' : _when(x.lastUsedAt!)),
            Text(x.searchable ? 'yes' : 'no'),
          ],
      ],
    );
  }
}

/// _Since : How far back the events are read.
///
/// A window rather than a count, because the question being asked of
/// this page is nearly always about a stretch of time -- what
/// happened this morning, what has the phone been doing today -- and
/// a count of two hundred answers a different question depending on
/// how busy the day was.
enum _Since {
  today('Today', 1),
  week('7 days', 7),
  month('4 weeks', 28),
  ever('Everything', 0);

  const _Since(this.label, this.days);
  final String label;
  final int days;
}

class _Events extends StatefulWidget {
  const _Events({
    required this.state,
    required this.search,
    required this.kind,
    required this.onKind,
  });

  final AppState state;
  final String search;
  final String kind;
  final ValueChanged<String> onKind;

  @override
  State<_Events> createState() => _EventsState();
}

class _EventsState extends State<_Events> {
  _Since _since = _Since.ever;

  /// _value : The one field worth a column of its own.
  ///
  /// Every sender writes what the thing was into "value" -- the name
  /// of a caller, the network, the artist, the battery level -- and
  /// read as part of a run of key: value pairs it is the hardest
  /// thing on the row to find. Here it is the row.
  static String _value(Map<String, dynamic> p) =>
      (p['value'] ?? '').toString().trim();

  /// _rest : Everything else the sender said, without the two that
  /// say nothing.
  ///
  /// "value" has its own column. "via" repeats the source: every row
  /// of a phone's events read "via: phone" beside "tasker", which is
  /// one fact taking two columns and crowding out the ones that
  /// differ.
  ///
  /// Named with a colon and shown in full. Every field is here on
  /// purpose: the number a call came from and how long it lasted
  /// were the two most useful things on the page and both were
  /// behind an ellipsis.
  static String _rest(Map<String, dynamic> p) => p.entries
      .where((x) => x.key != 'value' && x.key != 'via')
      .map((x) => '${x.key}: ${x.value}')
      .join('   ·   ');

  @override
  Widget build(BuildContext context) {
    final e = widget.state.events;
    if (e == null) return const SizedBox.shrink();
    final search = widget.search;

    final items = e.items
        .where(
          (x) =>
              search.isEmpty ||
              x.kind.toLowerCase().contains(search) ||
              _value(x.payload).toLowerCase().contains(search) ||
              _rest(x.payload).toLowerCase().contains(search),
        )
        .toList();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // When, then what. Two rows of chips rather than one list,
        // because they narrow different things and mixing them reads
        // as one long list of unrelated choices.
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.xs,
          children: [
            for (final s in _Since.values)
              ChoiceChip(
                label: Text(s.label),
                selected: _since == s,
                onSelected: (_) {
                  setState(() => _since = s);
                  widget.state.loadEvents(kind: widget.kind, days: s.days);
                },
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.xs),
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.xs,
          children: [
            ChoiceChip(
              label: const Text('everything'),
              selected: widget.kind.isEmpty,
              onSelected: (_) {
                widget.onKind('');
                widget.state.loadEvents(days: _since.days);
              },
            ),
            for (final k in e.kinds)
              ChoiceChip(
                label: Text('${k.kind}  ${k.count}'),
                selected: widget.kind == k.kind,
                onSelected: (_) {
                  widget.onKind(k.kind);
                  widget.state.loadEvents(kind: k.kind, days: _since.days);
                },
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        _Table(
          summary: [
            '${items.length} shown',
            if (_since != _Since.ever) _since.label.toLowerCase(),
            if (widget.kind.isNotEmpty) widget.kind,
          ].join(' · '),
          columns: const [
            '',
            'When',
            'What',
            'Kind',
            'Detail',
            'From',
            'Arrived',
          ],
          tall: true,
          empty: const AppEmptyState(
            icon: Icons.sensors_outlined,
            title: 'Nothing to show',
            body:
                'Events arrive from your own devices. Nobody has to ask '
                'for them.',
          ),
          rows: [
            for (final x in items)
              [
                Icon(
                  _icon(x.kind),
                  size: 18,
                  color: _tint(context, x.kind, _value(x.payload)),
                ),
                _cell(context, _when(x.occurredAt), width: 104),
                // The thing itself, in the reading weight of the
                // page rather than the muted weight of a detail.
                _wrapped(
                  context,
                  _value(x.payload).isEmpty ? '—' : _value(x.payload),
                  width: 200,
                  strong: true,
                  colour: _tint(context, x.kind, _value(x.payload)),
                ),
                _cell(context, x.kind, width: 150),
                _wrapped(context, _rest(x.payload), width: 320),
                _cell(
                  context,
                  x.device.isEmpty ? x.source : '${x.source} · ${x.device}',
                  width: 140,
                ),
                // The gap between happening and arriving is the one number
                // that says the device had been offline. Shown only when it
                // is real: "0s later" on every row would bury the ones that
                // matter.
                _cell(
                  context,
                  x.lateBy > 60 ? '${_late(x.lateBy)} later' : 'at once',
                  width: 110,
                ),
              ],
          ],
        ),
      ],
    );
  }
}

class _Tools extends StatelessWidget {
  const _Tools({required this.state, required this.search});

  final AppState state;
  final String search;

  @override
  Widget build(BuildContext context) {
    final a = state.abilities;
    if (a == null) return const SizedBox.shrink();

    final rows = <List<Widget>>[];
    var shown = 0;
    for (final m in a.modules) {
      for (final t in m.tools) {
        if (search.isNotEmpty &&
            !t.name.toLowerCase().contains(search) &&
            !t.purpose.toLowerCase().contains(search) &&
            !m.domain.toLowerCase().contains(search)) {
          continue;
        }
        shown++;
        rows.add([
          _cell(context, m.domain, width: 130),
          _cell(context, t.name, width: 210),
          _cell(context, t.purpose, width: 380),
          Text(t.always ? 'always' : 'on request'),
          Text(t.writes ? 'writes' : 'reads'),
        ]);
      }
    }

    return _Table(
      summary: [
        '$shown of ${a.total} shown',
        '${a.always} described in every request',
        '${a.total - a.always} described only when asked about',
      ].join(' · '),
      columns: const ['Module', 'Tool', 'Purpose', 'Offered', 'Effect'],
      empty: const AppEmptyState(
        icon: Icons.handyman_outlined,
        title: 'Nothing to show',
        body: 'This client has not been offered any tools.',
      ),
      rows: rows,
    );
  }
}

class _Words extends StatelessWidget {
  const _Words({required this.state, required this.search});

  final AppState state;
  final String search;

  @override
  Widget build(BuildContext context) {
    final v = state.vocabulary;
    if (v == null) return const SizedBox.shrink();

    final terms = v.terms
        .where((t) => search.isEmpty || t.toLowerCase().contains(search))
        .toList();

    return _Table(
      summary: [
        '${terms.length} of ${v.count} shown',
        if (v.writtenAt != null) 'collected ${_when(v.writtenAt!)}',
      ].join(' · '),
      columns: const ['Term'],
      empty: const AppEmptyState(
        icon: Icons.record_voice_over_outlined,
        title: 'Nothing to show',
        body: 'Proper nouns are collected from conversations overnight.',
      ),
      rows: [
        for (final t in terms) [_cell(context, t, width: 360)],
      ],
    );
  }
}

/// _Unasked : What the assistant considered saying without being
/// asked, and what came of it.
///
/// Mostly things it decided not to say, which is the point. The
/// ordinary output of the whole feature is silence, and silence has
/// four causes -- nothing unusual arrived, the moment was refused,
/// the model declined, or the watcher has stopped running. From
/// outside they are identical, and the last one is the one worth
/// knowing about.
class _Unasked extends StatelessWidget {
  const _Unasked({required this.state, required this.search});

  final AppState state;
  final String search;

  @override
  Widget build(BuildContext context) {
    final r = state.remarks;
    if (r == null) return const SizedBox.shrink();

    final rows = r.remarks
        .where(
          (x) =>
              search.isEmpty ||
              x.label.toLowerCase().contains(search) ||
              x.said.toLowerCase().contains(search) ||
              x.outcome.toLowerCase().contains(search),
        )
        .toList();

    final spoke = r.remarks.where((x) => x.outcome == 'said').length;

    return _Table(
      summary: ['${rows.length} considered', '$spoke said aloud'].join(' · '),
      columns: const ['When', 'About', 'Why', 'Outcome', 'What happened'],
      empty: const AppEmptyState(
        icon: Icons.hearing_outlined,
        title: 'Nothing considered yet',
        body:
            'A row appears whenever something unusual arrives — whether or '
            'not it was worth saying out loud.',
      ),
      rows: [
        for (final x in rows)
          [
            _cell(context, _when(x.at), width: 110),
            _cell(context, x.label, width: 240),
            _cell(context, x.reason, width: 90),
            _cell(context, x.outcome, width: 100),
            _cell(context, x.said.isNotEmpty ? x.said : x.because, width: 420),
          ],
      ],
    );
  }
}

/// _when : A short, local rendering of a moment.
String _when(DateTime at) {
  final now = DateTime.now();
  if (at.year == now.year && at.month == now.month && at.day == now.day) {
    return _clock(at);
  }
  return '${at.day}/${at.month} ${_clock(at)}';
}

/// _clock : The time the way it is said, not the way a timetable
/// prints it. Twenty past ten at night is 10:20 pm here.
String _clock(DateTime at) {
  final hour = at.hour % 12 == 0 ? 12 : at.hour % 12;
  final minute = at.minute.toString().padLeft(2, '0');
  return '$hour:$minute ${at.hour < 12 ? 'am' : 'pm'}';
}

/// _icon : What the event was, at a glance.
///
/// Keyed off the family rather than the whole kind, so a new
/// battery or music event arrives with the right picture and
/// nothing here has to be edited. An unknown family gets the
/// generic one rather than nothing, which would make the column
/// ragged.
Color _tint(BuildContext context, String kind, String value) {
  final t = context.tokens.palette;
  switch (kind) {
    case 'call.missed':
    case 'call.unanswered':
    case 'battery.low':
      return t.danger;
    case 'battery.full':
    case 'power.plugged':
    case 'call.received':
      return t.success;
    case 'power.unplugged':
      return t.warning;
  }
  switch (kind.split('.').first) {
    // A level is read as a level: the number decides the colour, so
    // a glance down the column shows the day draining.
    case 'battery':
      final level = int.tryParse(value);
      if (level == null) return t.textSecondary;
      if (level <= 20) return t.danger;
      if (level <= 40) return t.warning;
      return t.success;
    case 'call':
      return t.accent;
    case 'music':
    case 'place':
    case 'location':
      return t.accent;
    case 'app':
      return t.textSecondary;
  }
  // Screen, wifi, bluetooth: the background noise of a phone. Kept
  // quiet so the rows that mean something stand out.
  return t.textMuted;
}

IconData _icon(String kind) {
  final family = kind.split('.').first;
  switch (kind) {
    case 'screen.locked':
      return Icons.lock_outline;
    case 'screen.unlocked':
      return Icons.lock_open_outlined;
    case 'call.missed':
      return Icons.phone_missed_outlined;
    case 'call.received':
      return Icons.phone_callback_outlined;
    case 'call.outgoing':
      return Icons.phone_forwarded_outlined;
    case 'battery.low':
      return Icons.battery_alert_outlined;
    case 'battery.full':
      return Icons.battery_full_outlined;
  }
  switch (family) {
    case 'battery':
      return Icons.battery_std_outlined;
    case 'power':
      return Icons.power_outlined;
    case 'screen':
      return Icons.smartphone_outlined;
    case 'call':
      return Icons.call_outlined;
    case 'music':
      return Icons.music_note_outlined;
    case 'place':
      return Icons.place_outlined;
    case 'location':
      return Icons.my_location_outlined;
    case 'app':
      return Icons.apps_outlined;
    case 'network':
    case 'wifi':
      return Icons.wifi_outlined;
    case 'message':
    case 'sms':
      return Icons.sms_outlined;
    case 'reminder':
      return Icons.alarm_outlined;
  }
  return Icons.sensors_outlined;
}

/// _wrapped : A cell that grows to fit rather than cutting the
/// value off.
///
/// The whole point of the detail column is the part that was being
/// hidden behind an ellipsis: a caller's number, how long a call
/// lasted. A row that is one line taller is a cheap price for
/// being able to read it.
Widget _wrapped(
  BuildContext context,
  String text, {
  required double width,
  bool strong = false,
  Color? colour,
}) {
  final style = Theme.of(context).textTheme.bodyMedium;
  return SizedBox(
    width: width,
    child: Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
      child: Text(
        text,
        softWrap: true,
        style: style?.copyWith(
          fontWeight: strong ? FontWeight.w600 : null,
          color: colour,
        ),
      ),
    ),
  );
}

/// _late : How long a device had been unable to reach anything.
String _late(int seconds) {
  if (seconds < 3600) return '${(seconds / 60).round()} min';
  if (seconds < 86400) return '${(seconds / 3600).round()} h';
  return '${(seconds / 86400).round()} d';
}
