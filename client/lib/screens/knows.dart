/// What the assistant knows, and what it can do.
///
/// Everything here is read-only and nothing here is new information to the
/// server — it is all things that already decided how the assistant
/// behaves, with no way to look at them. A memory is believed
/// indefinitely and corrected only by accident. Half the tools are
/// withheld from any given request by a mechanism nothing displays. The
/// events arrive on their own and nothing has ever read them back.
///
/// Four sections rather than four pages: they are each small, and the
/// question they answer together — what does it know about me — is one
/// question.
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
    setState(() => _section = section);
    _load(section);
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Wrap(
          spacing: AppSpacing.sm,
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
        Expanded(
          child: AnimatedBuilder(
            animation: widget.state,
            builder: (context, _) {
              if (widget.state.busy) {
                return const Center(child: AppSpinner());
              }
              return switch (_section) {
                KnowsSection.memories => _Memories(state: widget.state),
                KnowsSection.events => _Events(
                  state: widget.state,
                  kind: _kind,
                  onKind: (k) {
                    setState(() => _kind = k);
                    widget.state.loadEvents(kind: k);
                  },
                ),
                KnowsSection.tools => _Tools(state: widget.state),
                KnowsSection.vocabulary => _Words(state: widget.state),
              };
            },
          ),
        ),
      ],
    );
  }
}

/// _Counts : The one-line summary above a section.
class _Counts extends StatelessWidget {
  const _Counts(this.parts);

  final List<String> parts;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.md),
    child: Text(
      parts.join(' · '),
      style: Theme.of(context).textTheme.bodySmall,
    ),
  );
}

class _Memories extends StatelessWidget {
  const _Memories({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final m = state.memories;
    if (m == null || m.items.isEmpty) {
      return const AppEmptyState(
        icon: Icons.psychology_outlined,
        title: 'Nothing remembered yet',
        body: 'Memories are written during a conversation, when something '
            'is worth keeping.',
      );
    }
    final theme = Theme.of(context);
    return ListView(
      children: [
        _Counts([
          '${m.items.length} remembered',
          '${m.always} in every prompt',
          if (m.unused > 0) '${m.unused} never used',
        ]),
        for (final item in m.items)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.md),
            child: AppSurface(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          item.subject,
                          style: theme.textTheme.titleSmall,
                        ),
                      ),
                      if (item.always)
                        const _Tag('always', emphasis: true)
                      else
                        const _Tag('recall'),
                    ],
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  Text(item.body, style: theme.textTheme.bodyMedium),
                  const SizedBox(height: AppSpacing.sm),
                  Text(
                    [
                      item.uses == 0 ? 'never used' : 'used ${item.uses}×',
                      if (!item.searchable) 'not searchable by meaning',
                    ].join(' · '),
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ),
      ],
    );
  }
}

class _Events extends StatelessWidget {
  const _Events({required this.state, required this.kind, required this.onKind});

  final AppState state;
  final String kind;
  final ValueChanged<String> onKind;

  @override
  Widget build(BuildContext context) {
    final e = state.events;
    if (e == null || (e.items.isEmpty && e.kinds.isEmpty)) {
      return const AppEmptyState(
        icon: Icons.sensors_outlined,
        title: 'Nothing reported yet',
        body: 'Events arrive from your own devices. Nobody has to ask for '
            'them.',
      );
    }
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
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
        Expanded(
          child: ListView(
            children: [
              for (final item in e.items)
                Padding(
                  padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                  child: AppSurface(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                item.kind,
                                style: theme.textTheme.titleSmall,
                              ),
                            ),
                            Text(
                              _when(item.occurredAt),
                              style: theme.textTheme.bodySmall,
                            ),
                          ],
                        ),
                        if (item.payload.isNotEmpty) ...[
                          const SizedBox(height: AppSpacing.xs),
                          Text(
                            item.payload.entries
                                .map((p) => '${p.key}: ${p.value}')
                                .join('   '),
                            style: theme.textTheme.bodyMedium,
                          ),
                        ],
                        const SizedBox(height: AppSpacing.xs),
                        Text(
                          [
                            '${item.source}${item.device.isEmpty ? '' : ' · ${item.device}'}',
                            // The gap between happening and arriving is the
                            // one number that says the device had been
                            // offline. Only worth showing when it is real.
                            if (item.lateBy > 60) 'arrived ${_late(item.lateBy)} later',
                          ].join(' · '),
                          style: theme.textTheme.bodySmall,
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _Tools extends StatelessWidget {
  const _Tools({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final a = state.abilities;
    if (a == null || a.modules.isEmpty) {
      return const AppEmptyState(
        icon: Icons.handyman_outlined,
        title: 'No tools',
        body: 'This client has not been offered any.',
      );
    }
    final theme = Theme.of(context);
    return ListView(
      children: [
        _Counts([
          '${a.total} tools',
          '${a.always} described in every request',
          '${a.total - a.always} described on request',
        ]),
        for (final m in a.modules)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.md),
            child: AppSurface(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(m.domain, style: theme.textTheme.titleSmall),
                  const SizedBox(height: AppSpacing.sm),
                  for (final t in m.tools)
                    Padding(
                      padding: const EdgeInsets.only(bottom: AppSpacing.xs),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(t.name, style: theme.textTheme.bodyMedium),
                                if (t.purpose.isNotEmpty)
                                  Text(
                                    t.purpose,
                                    style: theme.textTheme.bodySmall,
                                  ),
                              ],
                            ),
                          ),
                          if (t.writes) const _Tag('writes'),
                          if (t.always) const _Tag('always', emphasis: true),
                        ],
                      ),
                    ),
                ],
              ),
            ),
          ),
      ],
    );
  }
}

class _Words extends StatelessWidget {
  const _Words({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final v = state.vocabulary;
    if (v == null || v.terms.isEmpty) {
      return const AppEmptyState(
        icon: Icons.record_voice_over_outlined,
        title: 'No vocabulary',
        body: 'Proper nouns are collected from conversations overnight.',
      );
    }
    return ListView(
      children: [
        _Counts([
          '${v.count} terms',
          if (v.writtenAt != null) 'collected ${_when(v.writtenAt!)}',
        ]),
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: [for (final t in v.terms) _Tag(t)],
        ),
      ],
    );
  }
}

/// _Tag : A small label.
class _Tag extends StatelessWidget {
  const _Tag(this.text, {this.emphasis = false});

  final String text;
  final bool emphasis;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      margin: const EdgeInsets.only(left: AppSpacing.xs),
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.sm,
        vertical: AppSpacing.xxs,
      ),
      decoration: BoxDecoration(
        color: emphasis
            ? scheme.primary.withValues(alpha: 0.14)
            : scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(AppRadius.pill),
      ),
      child: Text(
        text,
        style: Theme.of(context).textTheme.bodySmall?.copyWith(
          color: emphasis ? scheme.primary : null,
        ),
      ),
    );
  }
}

/// _when : A short, local rendering of a moment.
String _when(DateTime at) {
  final now = DateTime.now();
  final sameDay =
      at.year == now.year && at.month == now.month && at.day == now.day;
  final hhmm =
      '${at.hour.toString().padLeft(2, '0')}:${at.minute.toString().padLeft(2, '0')}';
  if (sameDay) return hhmm;
  return '${at.day}/${at.month} $hhmm';
}

/// _late : How long a device had been unable to reach anything.
String _late(int seconds) {
  if (seconds < 3600) return '${(seconds / 60).round()} min';
  if (seconds < 86400) return '${(seconds / 3600).round()} h';
  return '${(seconds / 86400).round()} d';
}
