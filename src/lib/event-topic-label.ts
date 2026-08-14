type EventTopicsDictionary = {
  template: string;
  entities: Record<string, string>;
  actions: Record<string, string>;
};

// Topics look like "organization.created" or "repository.invitation.created":
// the first segment names the entity and the rest names the action.
export function formatEventTopic(dictionary: EventTopicsDictionary, topic: string): string {
  const separator = topic.indexOf(".");
  if (separator < 0) return topic;
  const entity = dictionary.entities[topic.slice(0, separator)];
  const action = dictionary.actions[topic.slice(separator + 1).replaceAll(".", "_")];
  if (!entity || !action) return topic.replaceAll(".", " ");
  return dictionary.template.replace("{entity}", entity).replace("{action}", action);
}
