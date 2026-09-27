/// Who is signed in, what else holds a token, and where the server is.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import '../design/design.dart';
import '../state/app_state.dart';

/// SettingsModule : One page of settings, named down the side.
enum SettingsModule {
  account('Account', Icons.person_outline),
  clients('Clients', Icons.devices_other_outlined),
  reminders('Reminders', Icons.alarm_outlined),
  voice('Voice', Icons.mic_none_outlined),
  server('Server', Icons.dns_outlined);

  const SettingsModule(this.title, this.icon);

  final String title;
  final IconData icon;
}

/// SettingsScreen : The settings, one module at a time.
///
/// Separate pages rather than one scrolling list: clients is the only part
/// that is a list of things to act on, and putting it under the account
/// fields meant scrolling past them to reach it. Down a side it can also
/// grow — conversations and voice belong here eventually — without the page
/// getting longer.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.state,
    this.module = SettingsModule.account,
    this.onModule,
  });

  /// module : Which page to show, which comes from the address.
  final SettingsModule module;

  /// onModule : Told when another page is picked, so the address follows.
  final ValueChanged<SettingsModule>? onModule;

  final AppState state;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  @override
  void initState() {
    super.initState();
    widget.state.loadClients();
    widget.state.loadReminders();
    _read(widget.module);
  }

  @override
  void didUpdateWidget(SettingsScreen old) {
    super.didUpdateWidget(old);
    // The address changed under us -- the back button, or a link.
    if (widget.module != old.module) _read(widget.module);
  }

  /// _module : Which page is showing. It lives in the address, not here.
  SettingsModule get _module => widget.module;

  /// _read : Reads afresh whatever a page is about to show.
  ///
  /// Reminders change without this screen being told: one set by voice
  /// while the page is open would otherwise not appear until Refresh was
  /// pressed, and an empty list reads as nothing to show rather than as
  /// out of date.
  void _read(SettingsModule module) {
    if (module == SettingsModule.reminders) widget.state.loadReminders();
    if (module == SettingsModule.clients) widget.state.loadClients();
  }

  /// _pick : Moves to a page, and with it the address.
  void _pick(SettingsModule module) {
    widget.onModule?.call(module);
    _read(module);
  }

  Future<void> _confirmRevoke(Client client) async {
    final self = client.current;
    final ok = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: context.colors.surfaceRaised,
        title: Text(
          self ? 'Sign out everywhere?' : 'Revoke this client?',
          style: context.text.subtitle,
        ),
        content: Text(
          self
              ? 'This is the client you are using. Revoking it signs you out '
                    'here as well.'
              : 'Its token stops working immediately. Anything using it will '
                    'have to sign in again.',
          style: context.text.body,
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.ghost,
            onPressed: () => Navigator.of(context).pop(false),
          ),
          AppButton(
            label: 'Revoke',
            variant: AppButtonVariant.danger,
            onPressed: () => Navigator.of(context).pop(true),
          ),
        ],
      ),
    );
    if (ok ?? false) await widget.state.revoke(client.id);
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final compact = context.isCompact;

    return AnimatedBuilder(
      animation: state,
      builder: (context, _) {
        return Scaffold(
          backgroundColor: context.colors.background,
          appBar: AppBar(
            backgroundColor: context.colors.background,
            surfaceTintColor: Colors.transparent,
            elevation: 0,
            title: Text(
              compact ? _module.title : 'Settings',
              style: context.text.subtitle,
            ),
            iconTheme: IconThemeData(color: context.colors.textPrimary),
          ),
          body: SafeArea(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (!compact) ...[
                  SizedBox(
                    width: 200,
                    child: _ModuleList(selected: _module, onPick: _pick),
                  ),
                  const AppDivider(vertical: true),
                ],
                Expanded(
                  child: ListView(
                    padding: const EdgeInsets.all(AppSpacing.lg),
                    children: [
                      if (state.error != null) ...[
                        AppBanner(
                          message: state.error!,
                          actionLabel: 'Dismiss',
                          onAction: state.dismissError,
                        ),
                        const SizedBox(height: AppSpacing.lg),
                      ],
                      // On a narrow screen the modules are a row of chips
                      // above the page: a column beside it would leave
                      // neither enough width to read.
                      if (compact) ...[
                        _ModuleChips(selected: _module, onPick: _pick),
                        const SizedBox(height: AppSpacing.lg),
                      ],
                      switch (_module) {
                        SettingsModule.account => _AccountModule(state: state),
                        SettingsModule.clients => _ClientsModule(
                          state: state,
                          onRevoke: _confirmRevoke,
                        ),
                        SettingsModule.reminders => _RemindersModule(
                          state: state,
                        ),
                        SettingsModule.voice => _VoiceModule(state: state),
                        SettingsModule.server => _ServerModule(state: state),
                      },
                    ],
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

/// _ModuleList : The modules down the side.
class _ModuleList extends StatelessWidget {
  const _ModuleList({required this.selected, required this.onPick});

  final SettingsModule selected;
  final ValueChanged<SettingsModule> onPick;

  @override
  Widget build(BuildContext context) => ColoredBox(
    color: context.colors.surfaceSunken,
    child: ListView(
      padding: const EdgeInsets.all(AppSpacing.sm),
      children: [
        for (final m in SettingsModule.values)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.xxs),
            child: _ModuleTile(
              module: m,
              selected: m == selected,
              onTap: () => onPick(m),
            ),
          ),
      ],
    ),
  );
}

/// _ModuleTile : One name down the side.
class _ModuleTile extends StatelessWidget {
  const _ModuleTile({
    required this.module,
    required this.selected,
    required this.onTap,
  });

  final SettingsModule module;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.sm),
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: AppSpacing.sm + 2,
        ),
        decoration: BoxDecoration(
          color: selected ? colors.accentSoft : Colors.transparent,
          borderRadius: BorderRadius.circular(AppRadius.sm),
        ),
        child: Row(
          children: [
            Icon(
              module.icon,
              size: 16,
              color: selected ? colors.accent : colors.textMuted,
            ),
            const SizedBox(width: AppSpacing.sm),
            Text(
              module.title,
              style: context.text.body.copyWith(
                color: selected ? colors.textPrimary : colors.textSecondary,
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// _ModuleChips : The modules as a row, for a screen too narrow for a column.
class _ModuleChips extends StatelessWidget {
  const _ModuleChips({required this.selected, required this.onPick});

  final SettingsModule selected;
  final ValueChanged<SettingsModule> onPick;

  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    children: [
      for (final m in SettingsModule.values)
        InkWell(
          onTap: () => onPick(m),
          borderRadius: BorderRadius.circular(AppRadius.pill),
          child: Container(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.md,
              vertical: AppSpacing.xs,
            ),
            decoration: BoxDecoration(
              color: m == selected
                  ? context.colors.accentSoft
                  : context.colors.surfaceRaised,
              borderRadius: BorderRadius.circular(AppRadius.pill),
            ),
            child: Text(
              m.title,
              style: context.text.caption.copyWith(
                color: m == selected
                    ? context.colors.accent
                    : context.colors.textSecondary,
              ),
            ),
          ),
        ),
    ],
  );
}

/// _AccountModule : Who is signed in, and the way out.
class _AccountModule extends StatelessWidget {
  const _AccountModule({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final identity = state.identity;
    return _Section(
      title: 'Account',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _Row(label: 'Signed in as', value: identity?.user.username ?? '—'),
          _Row(
            label: 'This client',
            value: identity == null || identity.client.name.isEmpty
                ? '—'
                : identity.client.name,
          ),
          if (state.personas.options.isNotEmpty) ...[
            const SizedBox(height: AppSpacing.lg),
            _Field(
              label: 'Manner',
              child: _Select(
                value: state.personas.currentName,
                options: [
                  for (final p in state.personas.options)
                    _Option(
                      label: '${p.name}  ·  ${p.summary}',
                      selected: p.id == state.personas.current,
                      onTap: () => state.setPersona(p.id),
                    ),
                ],
              ),
            ),
            const SizedBox(height: AppSpacing.xs),
            Text(
              'How the assistant speaks, everywhere. It takes effect on the '
              'next thing you ask, and is remembered across restarts.',
              style: context.text.caption.copyWith(
                color: context.colors.textMuted,
              ),
            ),
          ],
          const SizedBox(height: AppSpacing.lg),
          AppButton(
            label: 'Sign out',
            variant: AppButtonVariant.secondary,
            icon: Icons.logout,
            // Signing out takes the address back to the conversation on
            // its own, so there is nothing to close here.
            onPressed: state.signOut,
          ),
        ],
      ),
    );
  }
}

/// _ServerModule : Where the assistant is.
class _ServerModule extends StatelessWidget {
  const _ServerModule({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) => _Section(
    title: 'Server',
    subtitle:
        'Fixed when the app is built. The web bundle is meant to be served '
        'by the assistant itself, so its own origin is the answer.',
    child: _Row(
      label: 'Address',
      value: state.api.baseUrl.toString(),
      monospace: true,
    ),
  );
}

/// _ClientsModule : Everything holding a token, and what each one is.
class _ClientsModule extends StatelessWidget {
  const _ClientsModule({required this.state, required this.onRevoke});

  final AppState state;
  final ValueChanged<Client> onRevoke;

  @override
  Widget build(BuildContext context) => _Section(
    title: state.showRevoked ? 'Revoked clients' : 'Clients',
    subtitle: state.showRevoked
        ? 'Kept so a revocation is visible rather than silently absent. None '
              'of these can sign in, and none can be brought back.'
        : 'Everything holding a token for this account. How a client\'s '
              'prompts arrive decides what the assistant may do about them, '
              'and each one can be answered by a different model \u2014 a '
              'spoken answer has to arrive quickly, while an app can wait for '
              'a better one.',
    action: InkWell(
      onTap: () => state.setShowRevoked(!state.showRevoked),
      borderRadius: BorderRadius.circular(AppRadius.xs),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.xxs),
        child: Text(
          state.showRevoked ? 'Show active' : 'Show revoked',
          style: context.text.caption.copyWith(color: context.colors.accent),
        ),
      ),
    ),
    child: state.clients.isEmpty
        ? Text(
            state.showRevoked ? 'Nothing revoked.' : 'Nothing to show.',
            style: context.text.caption.copyWith(
              color: context.colors.textMuted,
            ),
          )
        : Column(
            children: [
              for (final c in state.clients)
                _ClientRow(
                  client: c,
                  catalogue: state.catalogue,
                  onRevoke: c.revoked ? null : () => onRevoke(c),
                  onChannel: c.revoked
                      ? null
                      : (channel) => state.setClientChannel(c.id, channel),
                  onModel: c.revoked
                      ? null
                      : (model) => state.setClientModel(
                          c.id,
                          model?.vendor ?? '',
                          model?.id ?? '',
                        ),
                ),
            ],
          ),
  );
}

/// _Section : A titled card, so the page reads as a few groups rather than
/// one long list of fields.
/// _RemindersModule : What is waiting to be said, and a way to stop it.
///
/// Only what is still coming. Everything that ever fired is a log, and
/// nobody opens a settings screen to read one.
class _RemindersModule extends StatelessWidget {
  const _RemindersModule({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) => _Section(
    title: 'Reminders',
    subtitle:
        'Everything waiting to be said, soonest first, anything kept back '
        'until you were in the room, and anything that was never said at all. '
        'These are spoken through the voice satellite when their time comes, '
        'whether or not anything is open here. Show past adds the ones you '
        'have already been told.',
    action: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        _SectionLink(
          label: state.showPast ? 'Hide past' : 'Show past',
          onTap: state.togglePast,
        ),
        const SizedBox(width: AppSpacing.md),
        _SectionLink(label: 'Refresh', onTap: state.loadReminders),
      ],
    ),
    child: state.reminders.isEmpty
        ? Text(
            state.showPast
                ? 'Nothing waiting, and nothing has been said yet. Ask for a '
                      'timer or a reminder and it appears here.'
                : 'Nothing waiting. Ask for a timer or a reminder and it '
                      'appears here.',
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          )
        : Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final r in state.reminders)
                _ReminderTile(
                  reminder: r,
                  onCancel: () => state.cancelReminder(r.id),
                  onSnooze: (minutes) => _snooze(context, state, r, minutes),
                ),
            ],
          ),
  );
}

/// _snooze : Puts one off, and says so when what happened needs saying.
Future<void> _snooze(
  BuildContext context,
  AppState state,
  Reminder r,
  int minutes,
) async {
  final said = await state.snoozeReminder(r.id, minutes);
  if (said != null && context.mounted) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(said)));
  }
}

/// _VoiceModule : What speech-to-text is primed with.
///
/// Shown because the list is no longer written by hand: it is built from
/// reminders, conversation names and memories, so a word that was misheard
/// once and saved is primed from then on. "Kitla BMRs" is a real reminder
/// title and would teach the mistake. Nothing here can be edited, because
/// the fix is to correct whatever it came from -- rename the reminder and
/// the word goes.
class _VoiceModule extends StatefulWidget {
  const _VoiceModule({required this.state});

  final AppState state;

  @override
  State<_VoiceModule> createState() => _VoiceModuleState();
}

class _VoiceModuleState extends State<_VoiceModule> {
  @override
  void initState() {
    super.initState();
    // After the frame, because loading notifies listeners and doing that
    // during a build is what Flutter forbids.
    WidgetsBinding.instance.addPostFrameCallback(
      (_) => widget.state.loadVocabulary(),
    );
  }

  @override
  Widget build(BuildContext context) {
    final v = widget.state.vocabulary;
    return _Section(
      title: 'Speech vocabulary',
      subtitle:
          'Words the speech recogniser is told to expect before it hears '
          'anything. Without them it returns a clean, confident wrong word '
          'rather than a misspelling, which is why a name it has never met '
          'comes back as something else entirely.',
      action: _SectionLink(
        label: 'Refresh',
        onTap: widget.state.loadVocabulary,
      ),
      child: v == null
          ? Text(
              'Reading it.',
              style: context.text.caption.copyWith(
                color: context.colors.textSecondary,
              ),
            )
          : Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _WordGroup(
                  label: 'From your reminders, conversations and memories',
                  words: v.found,
                  empty:
                      'Nothing yet. These are collected from what you have '
                      'asked to be reminded of and what has been remembered '
                      'about you.',
                  note:
                      'A word here that you never said came from something '
                      'stored wrongly. Correct the reminder or memory it came '
                      'from and it goes.',
                ),
                const SizedBox(height: AppSpacing.lg),
                _WordGroup(
                  label: 'Primed by hand',
                  words: v.core,
                  empty: 'None.',
                ),
                const SizedBox(height: AppSpacing.lg),
                Text(
                  '\${v.used} of \${v.budget} characters used. Past the limit the '
                  'recogniser keeps the end of the list, so the hand-written '
                  'words go first — they are sent first to survive it.',
                  style: context.text.caption.copyWith(
                    color: context.colors.textSecondary,
                  ),
                ),
              ],
            ),
    );
  }
}

/// _WordGroup : One labelled set of words, wrapped as chips.
class _WordGroup extends StatelessWidget {
  const _WordGroup({
    required this.label,
    required this.words,
    required this.empty,
    this.note,
  });

  final String label;
  final List<String> words;
  final String empty;
  final String? note;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(label, style: context.text.label),
      const SizedBox(height: AppSpacing.sm),
      if (words.isEmpty)
        Text(
          empty,
          style: context.text.caption.copyWith(
            color: context.colors.textSecondary,
          ),
        )
      else
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: [for (final w in words) _Word(word: w)],
        ),
      if (note != null && words.isNotEmpty) ...[
        const SizedBox(height: AppSpacing.sm),
        Text(
          note!,
          style: context.text.caption.copyWith(
            color: context.colors.textSecondary,
          ),
        ),
      ],
    ],
  );
}

/// _Word : One primed word.
class _Word extends StatelessWidget {
  const _Word({required this.word});

  final String word;

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.symmetric(
      horizontal: AppSpacing.sm,
      vertical: AppSpacing.xs,
    ),
    decoration: BoxDecoration(
      color: context.colors.surfaceRaised,
      borderRadius: BorderRadius.circular(AppRadius.xs),
      border: Border.all(color: context.colors.border),
    ),
    child: Text(word, style: context.text.caption),
  );
}

/// _SectionLink : A word in the corner of a section that does something.
class _SectionLink extends StatelessWidget {
  const _SectionLink({required this.label, required this.onTap});

  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => InkWell(
    onTap: onTap,
    borderRadius: BorderRadius.circular(AppRadius.xs),
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.xxs),
      child: Text(
        label,
        style: context.text.caption.copyWith(color: context.colors.accent),
      ),
    ),
  );
}

/// _SnoozeButton : Put it off, by one of a few lengths.
///
/// A menu rather than a single Snooze, because the useful lengths are
/// nothing alike: ten minutes for one going off now, an hour or tomorrow
/// for one being got out of the way.
class _SnoozeButton extends StatelessWidget {
  const _SnoozeButton({required this.onSnooze});

  final ValueChanged<int> onSnooze;

  /// _lengths : What to offer, and what each is in minutes.
  static const _lengths = <String, int>{
    '10 minutes': 10,
    'An hour': 60,
    'This evening': 60 * 6,
    'Tomorrow': 60 * 24,
  };

  @override
  Widget build(BuildContext context) => PopupMenuButton<int>(
    tooltip: 'Put it off',
    position: PopupMenuPosition.under,
    color: context.colors.surface,
    onSelected: onSnooze,
    itemBuilder: (context) => [
      for (final length in _lengths.entries)
        PopupMenuItem(
          value: length.value,
          child: Text(
            length.key,
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          ),
        ),
    ],
    child: Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.sm,
        vertical: AppSpacing.xxs,
      ),
      child: Text(
        'Snooze',
        style: context.text.caption.copyWith(color: context.colors.textMuted),
      ),
    ),
  );
}

/// _ReminderTile : One thing waiting to be said.
class _ReminderTile extends StatelessWidget {
  const _ReminderTile({
    required this.reminder,
    required this.onCancel,
    required this.onSnooze,
  });

  final Reminder reminder;
  final VoidCallback onCancel;

  /// onSnooze : Put it off by this many minutes.
  final ValueChanged<int> onSnooze;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;

    // One that was never said is shown rather than hidden. Leaving it out
    // was how a reminder disappeared: not spoken, not here, nothing at
    // all to show it had existed.
    final missed = reminder.status == 'missed';
    // Already happened, one way or the other. Nothing to put off and
    // nothing to call off, so it is shown and left alone.
    final over = reminder.status == 'done' || reminder.status == 'cancelled';

    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.md),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            switch (reminder.status) {
              'missed' => Icons.notifications_off_outlined,
              'held' => Icons.pause_circle_outline,
              'done' => Icons.check,
              'cancelled' => Icons.close,
              _ => reminder.repeating ? Icons.repeat : Icons.alarm_outlined,
            },
            size: 15,
            color: missed ? colors.warning : colors.textMuted,
          ),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Flexible(
                      child: Text(reminder.title, style: context.text.body),
                    ),
                    const SizedBox(width: AppSpacing.sm),
                    Text(
                      switch (reminder.status) {
                        'missed' => 'missed, ${_when(reminder)}',
                        'held' => 'waiting for you, ${_when(reminder)}',
                        'done' => 'said ${_when(reminder)}',
                        'cancelled' => 'called off, was ${_when(reminder)}',
                        _ => _when(reminder),
                      },
                      style: context.text.caption.copyWith(
                        color: missed ? colors.warning : null,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: AppSpacing.xxs),
                // What it will actually say, because the name is for
                // finding it and this is what you will hear.
                Text(
                  reminder.say,
                  style: context.text.caption.copyWith(
                    color: colors.textSecondary,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          // Not offered on a missed one. Its time came and went unheard,
          // so there is nothing still to come to put off.
          if (!missed && !over) _SnoozeButton(onSnooze: onSnooze),
          // Nothing to do to one that has already happened. It is here
          // to be read, not acted on.
          if (!over)
            AppButton(
              // Nothing is called off about one that already failed to
              // happen; the only thing left is to stop looking at it.
              label: missed ? 'Dismiss' : 'Cancel',
              variant: AppButtonVariant.ghost,
              onPressed: onCancel,
            ),
        ],
      ),
    );
  }

  /// _when : When it happens, said the way a person would.
  ///
  /// A date on its own makes somebody work out whether it is soon. Today
  /// and tomorrow are what almost every reminder is.
  static String _when(Reminder r) {
    final at = r.dueAt;
    final clock = _clock(at);

    final today = DateUtils.dateOnly(DateTime.now());
    final day = DateUtils.dateOnly(at);
    final days = day.difference(today).inDays;

    final when = switch (days) {
      0 => 'today at $clock',
      1 => 'tomorrow at $clock',
      _ when days > 1 && days < 7 => '${_weekday(at)} at $clock',
      _ => '${at.day}/${at.month} at $clock',
    };
    return r.repeating ? '$when, ${r.repeats}' : when;
  }

  /// _clock : The time of day, without dragging in a locale package.
  static String _clock(DateTime at) =>
      '${at.hour.toString().padLeft(2, '0')}:'
      '${at.minute.toString().padLeft(2, '0')}';

  static String _weekday(DateTime at) => const [
    'Monday',
    'Tuesday',
    'Wednesday',
    'Thursday',
    'Friday',
    'Saturday',
    'Sunday',
  ][at.weekday - 1];
}

class _Section extends StatelessWidget {
  const _Section({
    required this.title,
    required this.child,
    this.subtitle,
    this.action,
  });

  final String title;
  final String? subtitle;
  final Widget child;

  /// action : Something to do with the whole section, shown beside its title.
  final Widget? action;

  @override
  Widget build(BuildContext context) => AppSurface(
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(child: Text(title, style: context.text.label)),
            ?action,
          ],
        ),
        if (subtitle != null) ...[
          const SizedBox(height: AppSpacing.xxs),
          Text(
            subtitle!,
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          ),
        ],
        const SizedBox(height: AppSpacing.md),
        child,
      ],
    ),
  );
}

/// _Row : A label with its value, wrapping rather than clipping.
class _Row extends StatelessWidget {
  const _Row({
    required this.label,
    required this.value,
    this.monospace = false,
  });

  final String label;
  final String value;
  final bool monospace;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SizedBox(
          width: 120,
          child: Text(
            label,
            style: context.text.caption.copyWith(
              color: context.colors.textSecondary,
            ),
          ),
        ),
        Expanded(
          child: SelectableText(
            value,
            style: monospace ? context.text.mono : context.text.body,
          ),
        ),
      ],
    ),
  );
}

/// _ClientRow : One client, with the way to take its token away.
class _ClientRow extends StatelessWidget {
  const _ClientRow({
    required this.client,
    this.catalogue = const ModelCatalogue(),
    this.onRevoke,
    this.onChannel,
    this.onModel,
  });

  final Client client;

  /// catalogue : What this client may be answered by, and what answers it
  /// when it has chosen nothing.
  final ModelCatalogue catalogue;

  final VoidCallback? onRevoke;

  /// onChannel : Called with "voice" or "direct".
  ///
  /// Offered because a client cannot always declare itself: Home Assistant is
  /// handed a token through a screen with no field for it, so its client
  /// registers as direct and has to be corrected here.
  final ValueChanged<String>? onChannel;

  /// onModel : Called with the model to answer this client, or null to put it
  /// back on the server's.
  final ValueChanged<LlmModel?>? onModel;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final name = client.name.isEmpty ? client.id : client.name;

    // A client cannot raise its own channel, so the field is shown as a fact
    // rather than as a control that refuses.
    final ownChannel = client.current;

    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(child: Text(name, style: context.text.body)),
                        if (client.current) ...[
                          const SizedBox(width: AppSpacing.sm),
                          _Tag('you are signed in here', color: colors.accent),
                        ],
                        if (client.revoked) ...[
                          const SizedBox(width: AppSpacing.sm),
                          _Tag('revoked', color: colors.textMuted),
                        ],
                      ],
                    ),
                    Text(
                      client.id,
                      style: context.text.caption.copyWith(
                        color: colors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              if (onRevoke != null)
                AppButton(
                  label: 'Revoke',
                  variant: AppButtonVariant.ghost,
                  compact: true,
                  onPressed: onRevoke,
                ),
            ],
          ),
          if (onChannel != null || onModel != null) ...[
            const SizedBox(height: AppSpacing.sm),
            Wrap(
              spacing: AppSpacing.xl,
              runSpacing: AppSpacing.sm,
              children: [
                if (onChannel != null)
                  _Field(
                    label: 'Prompts arrive',
                    child: _Select(
                      value: _channelLabel(client.channel),
                      hint: ownChannel
                          ? 'A client cannot change its own. Use another one.'
                          : null,
                      options: [
                        for (final entry in _channels.entries)
                          _Option(
                            label: entry.value,
                            selected: client.channel == entry.key,
                            onTap: ownChannel
                                ? null
                                : () => onChannel!(entry.key),
                          ),
                      ],
                    ),
                  ),
                if (onModel != null && catalogue.models.isNotEmpty)
                  _Field(
                    label: 'Answered by',
                    child: _Select(
                      value: _modelLabel(),
                      options: [
                        _Option(
                          label: catalogue.defaultName.isEmpty
                              ? 'Server default'
                              : 'Server default \u00b7 ${catalogue.defaultName}',
                          selected: client.model.isEmpty,
                          onTap: () => onModel!(null),
                        ),
                        for (final m in catalogue.models)
                          _Option(
                            label:
                                '${m.name}  \u00b7  '
                                '${_thousands(m.contextTokens)} tokens',
                            selected: m.id == client.model,
                            onTap: () => onModel!(m),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }

  /// _modelLabel : What the model field reads as when closed.
  ///
  /// A client that has chosen nothing shows the model that will answer it
  /// anyway, since "server default" on its own tells a person only that they
  /// have not been told.
  String _modelLabel() {
    for (final m in catalogue.models) {
      if (m.id == client.model) return m.name;
    }
    return catalogue.defaultName.isEmpty
        ? 'Server default'
        : '${catalogue.defaultName}  (server default)';
  }
}

/// _channels : How each channel is named to a person, in the order they are
/// offered.
const Map<String, String> _channels = {
  'voice': 'Spoken, through the satellite',
  'direct': 'Typed, through an app or the API',
};

/// _channelLabel : The short form, for the closed field.
String _channelLabel(String channel) => switch (channel) {
  'voice' => 'Spoken',
  'direct' => 'Typed',
  _ => channel,
};

/// _Tag : A short word beside a name, such as which client you are using.
class _Tag extends StatelessWidget {
  const _Tag(this.text, {required this.color});

  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) =>
      Text(text, style: context.text.caption.copyWith(color: color));
}

/// _Field : A labelled control, so a value is never a bare word whose meaning
/// has to be guessed from its position.
class _Field extends StatelessWidget {
  const _Field({required this.label, required this.child});

  final String label;
  final Widget child;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(
        label,
        style: context.text.caption.copyWith(color: context.colors.textMuted),
      ),
      const SizedBox(height: AppSpacing.xxs),
      child,
    ],
  );
}

/// _Option : One entry in a _Select. A null onTap is an entry that cannot be
/// chosen, which is how the already-selected one and a forbidden one are both
/// shown.
class _Option {
  const _Option({required this.label, required this.selected, this.onTap});

  final String label;
  final bool selected;
  final VoidCallback? onTap;
}

/// _Select : A value with a menu behind it.
///
/// The same control for the channel and for the model, because they are the
/// same kind of thing: one of a short list, and the list is worth reading
/// before choosing. Two words side by side said neither what they were nor
/// that they could be changed.
class _Select extends StatelessWidget {
  const _Select({required this.value, required this.options, this.hint});

  final String value;
  final List<_Option> options;

  /// hint : Why the field cannot be changed, when it cannot.
  final String? hint;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final locked = options.every((o) => o.onTap == null);

    return PopupMenuButton<int>(
      tooltip: hint ?? '',
      enabled: !locked,
      position: PopupMenuPosition.under,
      color: colors.surface,
      onSelected: (i) => options[i].onTap?.call(),
      itemBuilder: (context) => [
        for (var i = 0; i < options.length; i++)
          PopupMenuItem(
            value: i,
            enabled: options[i].onTap != null,
            child: Text(
              options[i].label,
              style: context.text.caption.copyWith(
                color: options[i].selected
                    ? colors.accent
                    : colors.textSecondary,
              ),
            ),
          ),
        if (hint != null)
          PopupMenuItem(
            enabled: false,
            child: Text(
              hint!,
              style: context.text.caption.copyWith(color: colors.textMuted),
            ),
          ),
      ],
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.sm,
          vertical: AppSpacing.xxs + 1,
        ),
        decoration: BoxDecoration(
          border: Border.all(color: colors.border),
          borderRadius: BorderRadius.circular(AppRadius.xs),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              value,
              style: context.text.caption.copyWith(
                color: locked ? colors.textMuted : colors.textPrimary,
              ),
            ),
            const SizedBox(width: AppSpacing.xxs),
            Icon(
              locked ? Icons.lock_outline : Icons.expand_more,
              size: 14,
              color: colors.textMuted,
            ),
          ],
        ),
      ),
    );
  }
}

/// _thousands : A count with separators, so 200000 reads as a size rather
/// than a string of noughts.
String _thousands(int n) {
  final digits = n.toString();
  final out = StringBuffer();
  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) out.write(',');
    out.write(digits[i]);
  }
  return out.toString();
}
