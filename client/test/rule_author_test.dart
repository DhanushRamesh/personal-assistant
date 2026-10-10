import 'package:flutter_test/flutter_test.dart';
import 'package:personal_assistant_client/api/models.dart';

Rule ruleOf(String kind, {String because = ''}) => Rule(
  id: 'r',
  name: 'a rule',
  kind: kind,
  schedule: 'every 5m',
  state: 'active',
  because: because,
);

void main() {
  test('a rule the person asked for carries no marker', () {
    final mine = ruleOf('rule');
    expect(mine.author, RuleAuthor.asked);
    expect(mine.his, isFalse);
  });

  test('the two he writes himself are told apart', () {
    expect(ruleOf('noticed').author, RuleAuthor.noticed);
    expect(ruleOf('remarked').author, RuleAuthor.remarked);
    expect(ruleOf('noticed').his, isTrue);
    expect(ruleOf('remarked').his, isTrue);
  });

  test('an unrecognised kind is the person own', () {
    // A rule wrongly marked as his invites deleting somebody else's
    // work on the strength of a label, so anything unknown is theirs.
    expect(ruleOf('reminder').author, RuleAuthor.asked);
    expect(ruleOf('').author, RuleAuthor.asked);
  });

  test('why and because are different fields', () {
    final r = Rule.fromJson({
      'id': 'r',
      'name': 'n',
      'kind': 'noticed',
      'schedule': 'every 5m',
      'state': 'active',
      'why': 'the condition did not hold',
      'because': 'charged on 8 of 9 nights',
    });
    expect(r.why, 'the condition did not hold');
    expect(r.because, 'charged on 8 of 9 nights');
    expect(r.his, isTrue);
  });
}
