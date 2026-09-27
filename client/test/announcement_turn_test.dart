import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/api/models.dart';
import 'package:personal_assistant_client/design/design.dart';

/// show : Puts one turn on screen.
Future<void> show(WidgetTester tester, Widget child) => tester.pumpWidget(
  MaterialApp(
    theme: AppTheme.dark,
    home: Scaffold(body: child),
  ),
);

void main() {
  // An announcement says what made the assistant speak, not "Assistant".
  // Nothing above it explains why it is there.
  testWidgets('an announcement is labelled by what prompted it', (
    tester,
  ) async {
    await show(
      tester,
      const AppTurn(
        speaker: AppSpeaker.assistant,
        text: 'Take your tablets',
        label: 'Reminder',
        unprompted: true,
      ),
    );

    expect(find.text('Reminder'), findsOneWidget);
    expect(find.text('announced'), findsOneWidget);
    expect(find.text('Assistant'), findsNothing);
    expect(find.text('Take your tablets'), findsOneWidget);
  });

  // An ordinary answer is unchanged.
  testWidgets('an answer is still the assistant', (tester) async {
    await show(
      tester,
      const AppTurn(speaker: AppSpeaker.assistant, text: 'It is half past.'),
    );

    expect(find.text('Assistant'), findsOneWidget);
    expect(find.text('announced'), findsNothing);
  });

  // The two kinds read differently, and an unknown one still reads.
  test('every kind has a label', () {
    expect(AnnouncementKind.parse('reminder').label, 'Reminder');
    expect(AnnouncementKind.parse('presence').label, 'Welcome');
    expect(AnnouncementKind.parse('something new').label, 'Announcement');
  });
}
