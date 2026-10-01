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
  vocabulary('Vocabulary');

  const KnowsSection(this.title);
  final String title;
}

/// KnowsModule : The settings page that shows all of it.
class KnowsModule extends StatefulWidget {
  const KnowsModule({super.key, required this.state});

  final AppState state;

  @override
  State<KnowsModule> createState() => _KnowsModuleState();
}

class _KnowsModuleState extends State<KnowsModule> {
  KnowsSection _section = KnowsSection.memories;
  String _kind = '';
  String _search = '';
  final _searchField = TextEditingController();

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
    }
  }

  void _show(KnowsSection section) {
    _searchField.clear();
    setState(() {
      _section = section;
      _search = '';
    });
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
          onChanged: (v) => setState(() => _search = v.trim().toLowerCase()),
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
                onKind: (k) {
                  setState(() => _kind = k);
                  widget.state.loadEvents(kind: k);
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
  });

  final String summary;
  final List<String> columns;
  final List<List<Widget>> rows;
  final Widget empty;

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
            dataRowMaxHeight: 72,
            columnSpacing: AppSpacing.xl,
            columns: [
              for (final c in columns)
                DataColumn(
                  label: Text(c, style: theme.textTheme.labelMedium),
                ),
            ],
            rows: [
              for (final r in rows) DataRow(cells: [for (final c in r) DataCell(c)]),
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
      columns: const ['Subject', 'Memory', 'Tier', 'Used', 'Last used', 'By meaning'],
      empty: const AppEmptyState(
        icon: Icons.psychology_outlined,
        title: 'Nothing to show',
        body: 'Memories are written during a conversation, when something '
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

class _Events extends StatelessWidget {
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
  Widget build(BuildContext context) {
    final e = state.events;
    if (e == null) return const SizedBox.shrink();

    String payload(Map<String, dynamic> p) =>
        p.entries.map((x) => '${x.key}: ${x.value}').join('   ');

    final items = e.items
        .where(
          (x) =>
              search.isEmpty ||
              x.kind.toLowerCase().contains(search) ||
              payload(x.payload).toLowerCase().contains(search),
        )
        .toList();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.xs,
          children: [
            ChoiceChip(
              label: const Text('everything'),
              selected: kind.isEmpty,
              onSelected: (_) => onKind(''),
            ),
            for (final k in e.kinds)
              ChoiceChip(
                label: Text('${k.kind}  ${k.count}'),
                selected: kind == k.kind,
                onSelected: (_) => onKind(k.kind),
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        _Table(
          summary: '${items.length} shown',
          columns: const ['Happened', 'Kind', 'Details', 'From', 'Arrived'],
          empty: const AppEmptyState(
            icon: Icons.sensors_outlined,
            title: 'Nothing to show',
            body: 'Events arrive from your own devices. Nobody has to ask '
                'for them.',
          ),
          rows: [
            for (final x in items)
              [
                Text(_when(x.occurredAt)),
                _cell(context, x.kind, width: 170),
                _cell(context, payload(x.payload), width: 300),
                _cell(
                  context,
                  x.device.isEmpty ? x.source : '${x.source} · ${x.device}',
                  width: 150,
                ),
                // The gap between happening and arriving is the one number
                // that says the device had been offline. Shown only when it
                // is real: "0s later" on every row would bury the ones that
                // matter.
                Text(x.lateBy > 60 ? '${_late(x.lateBy)} later' : 'at once'),
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

/// _when : A short, local rendering of a moment.
String _when(DateTime at) {
  final now = DateTime.now();
  final hhmm =
      '${at.hour.toString().padLeft(2, '0')}:${at.minute.toString().padLeft(2, '0')}';
  if (at.year == now.year && at.month == now.month && at.day == now.day) {
    return hhmm;
  }
  return '${at.day}/${at.month} $hhmm';
}

/// _late : How long a device had been unable to reach anything.
String _late(int seconds) {
  if (seconds < 3600) return '${(seconds / 60).round()} min';
  if (seconds < 86400) return '${(seconds / 3600).round()} h';
  return '${(seconds / 86400).round()} d';
}
