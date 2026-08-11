import { useEffect, useState } from "react";
import {
  connectKafka,
  createKafkaProfile,
  deleteKafkaProfile,
  describeKafkaTopic,
  disconnectKafka,
  getKafkaConnectionStatus,
  listKafkaConsumerGroups,
  listKafkaProfiles,
  listKafkaTopics,
  peekKafkaMessages,
  publishKafkaMessage,
  testKafkaConnection,
  type KafkaConnectionStatus,
  type KafkaConsumerGroup,
  type KafkaMessage,
  type KafkaProfile,
  type KafkaTopic,
  type KafkaTopicDetail,
  type PublishOutcome,
} from "../../bridge/kafka";
import { listEnvTags, type EnvTag } from "../../bridge/environments";
import { errorMessage } from "../../bridge/errors";

const SECURITY_PROTOCOLS = ["PLAINTEXT", "SSL", "SASL_PLAINTEXT", "SASL_SSL"] as const;
const SASL_MECHANISMS = ["PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"] as const;

const emptyForm = {
  name: "",
  bootstrap: "127.0.0.1:9092",
  securityProtocol: "PLAINTEXT",
  saslMechanism: "PLAIN",
  username: "",
  secret: "",
  env: "dev",
};

function isSaslProtocol(protocol: string): boolean {
  return protocol === "SASL_PLAINTEXT" || protocol === "SASL_SSL";
}

export function KafkaView() {
  const [profiles, setProfiles] = useState<KafkaProfile[]>([]);
  const [envTags, setEnvTags] = useState<EnvTag[]>([]);
  const [form, setForm] = useState({ ...emptyForm });
  const [selectedId, setSelectedId] = useState("");
  const [connectionStatus, setConnectionStatus] = useState<KafkaConnectionStatus>({
    connected: false,
  });
  const [topics, setTopics] = useState<KafkaTopic[]>([]);
  const [selectedTopic, setSelectedTopic] = useState("");
  const [topicDetail, setTopicDetail] = useState<KafkaTopicDetail | null>(null);
  const [peekPartition, setPeekPartition] = useState(0);
  const [peekLimit, setPeekLimit] = useState(50);
  const [peekOffset, setPeekOffset] = useState(-1);
  const [messages, setMessages] = useState<KafkaMessage[]>([]);
  const [consumerGroups, setConsumerGroups] = useState<KafkaConsumerGroup[]>([]);
  const [publishTopic, setPublishTopic] = useState("");
  const [publishKey, setPublishKey] = useState("");
  const [publishPayload, setPublishPayload] = useState("");
  const [publishPartition, setPublishPartition] = useState("");
  const [publishResult, setPublishResult] = useState<PublishOutcome | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  async function refreshProfiles() {
    const [list, tags, conn] = await Promise.all([
      listKafkaProfiles(),
      listEnvTags(),
      getKafkaConnectionStatus(),
    ]);
    setProfiles(list);
    setEnvTags(tags);
    setConnectionStatus(conn);
    if (!selectedId && list.length > 0) {
      setSelectedId(list[0].id);
    }
  }

  async function refreshConnectedData() {
    if (!connectionStatus.connected) {
      setTopics([]);
      setTopicDetail(null);
      setMessages([]);
      setConsumerGroups([]);
      return;
    }
    try {
      const [topicList, groups] = await Promise.all([
        listKafkaTopics(),
        listKafkaConsumerGroups(),
      ]);
      setTopics(topicList);
      setConsumerGroups(groups);
      if (!selectedTopic && topicList.length > 0) {
        setSelectedTopic(topicList[0].name);
      }
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refreshProfiles().catch((e) => setError(errorMessage(e)));
  }, []);

  useEffect(() => {
    void refreshConnectedData();
  }, [connectionStatus.connected]);

  useEffect(() => {
    if (!connectionStatus.connected || !selectedTopic) {
      setTopicDetail(null);
      return;
    }
    void describeKafkaTopic(selectedTopic)
      .then((detail) => {
        setTopicDetail(detail);
        if (detail.partitions.length > 0) {
          setPeekPartition(detail.partitions[0]);
        }
        setPublishTopic(selectedTopic);
      })
      .catch((e) => setError(errorMessage(e)));
  }, [connectionStatus.connected, selectedTopic]);

  async function onCreate() {
    setError("");
    setStatus("");
    try {
      const bootstrapServers = form.bootstrap
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      const created = await createKafkaProfile({
        name: form.name,
        bootstrapServers,
        securityProtocol: form.securityProtocol,
        saslMechanism: isSaslProtocol(form.securityProtocol) ? form.saslMechanism : "",
        username: isSaslProtocol(form.securityProtocol) ? form.username : "",
        secret: form.secret,
        env: form.env,
      });
      setForm({ ...emptyForm });
      setSelectedId(created.id);
      await refreshProfiles();
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  async function onTest() {
    setError("");
    setStatus("Testing…");
    try {
      await testKafkaConnection(selectedId);
      setStatus("Connection OK");
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    }
  }

  async function onConnect() {
    setError("");
    setStatus("Connecting…");
    try {
      await connectKafka(selectedId);
      const conn = await getKafkaConnectionStatus();
      setConnectionStatus(conn);
      setStatus("Connected");
      await refreshConnectedData();
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    }
  }

  async function onDisconnect() {
    setError("");
    setStatus("Disconnecting…");
    try {
      await disconnectKafka();
      const conn = await getKafkaConnectionStatus();
      setConnectionStatus(conn);
      setSelectedTopic("");
      setPublishResult(null);
      setStatus("Disconnected");
    } catch (e) {
      setStatus("");
      setError(errorMessage(e));
    }
  }

  async function onPeek() {
    if (!selectedTopic) return;
    setError("");
    setStatus("Peeking messages…");
    try {
      setMessages(
        await peekKafkaMessages(selectedTopic, peekPartition, peekLimit, peekOffset),
      );
      setStatus("");
    } catch (e) {
      setStatus("");
      setMessages([]);
      setError(errorMessage(e));
    }
  }

  async function onPublish() {
    setError("");
    setStatus("Publishing…");
    try {
      const partition =
        publishPartition.trim() === "" ? undefined : Number(publishPartition);
      const result = await publishKafkaMessage({
        topic: publishTopic,
        key: publishKey || undefined,
        value: publishPayload,
        partition,
      });
      setPublishResult(result);
      setStatus(
        `Published to partition ${result.partition} at offset ${result.offset}`,
      );
    } catch (e) {
      setStatus("");
      setPublishResult(null);
      setError(errorMessage(e));
    }
  }

  const selectedProfile = profiles.find((p) => p.id === selectedId);

  return (
    <section className="kafka-view">
      <h2>Kafka</h2>

      <div className="kafka-create">
        <input
          aria-label="kafka-name"
          placeholder="Profile name"
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />
        <input
          aria-label="kafka-bootstrap"
          placeholder="Bootstrap servers (comma-separated)"
          value={form.bootstrap}
          onChange={(e) => setForm({ ...form, bootstrap: e.target.value })}
        />
        <select
          aria-label="kafka-security-protocol"
          value={form.securityProtocol}
          onChange={(e) => setForm({ ...form, securityProtocol: e.target.value })}
        >
          {SECURITY_PROTOCOLS.map((protocol) => (
            <option key={protocol} value={protocol}>
              {protocol}
            </option>
          ))}
        </select>
        {isSaslProtocol(form.securityProtocol) ? (
          <>
            <select
              aria-label="kafka-sasl-mechanism"
              value={form.saslMechanism}
              onChange={(e) => setForm({ ...form, saslMechanism: e.target.value })}
            >
              {SASL_MECHANISMS.map((mechanism) => (
                <option key={mechanism} value={mechanism}>
                  {mechanism}
                </option>
              ))}
            </select>
            <input
              aria-label="kafka-username"
              placeholder="SASL username"
              value={form.username}
              onChange={(e) => setForm({ ...form, username: e.target.value })}
            />
          </>
        ) : null}
        <input
          aria-label="kafka-secret"
          type="password"
          placeholder={isSaslProtocol(form.securityProtocol) ? "SASL password" : "Secret (optional)"}
          value={form.secret}
          onChange={(e) => setForm({ ...form, secret: e.target.value })}
        />
        <select
          aria-label="kafka-env"
          value={form.env}
          onChange={(e) => setForm({ ...form, env: e.target.value })}
        >
          {envTags.map((tag) => (
            <option key={tag.id} value={tag.name}>
              {tag.name}
            </option>
          ))}
        </select>
        <button type="button" onClick={onCreate}>
          Save connection
        </button>
      </div>

      <div className="kafka-profiles">
        <label>
          Connection
          <select
            aria-label="kafka-profile-select"
            value={selectedId}
            onChange={(e) => setSelectedId(e.target.value)}
          >
            <option value="">— select —</option>
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name} ({p.bootstrapServers.join(", ")})
              </option>
            ))}
          </select>
        </label>
        <button type="button" onClick={onTest} disabled={!selectedId}>
          Test connection
        </button>
        <button
          type="button"
          onClick={onConnect}
          disabled={!selectedId || connectionStatus.connected}
        >
          Connect
        </button>
        <button
          type="button"
          onClick={onDisconnect}
          disabled={!connectionStatus.connected}
        >
          Disconnect
        </button>
        {selectedId ? (
          <button
            type="button"
            aria-label="kafka-delete"
            onClick={async () => {
              await deleteKafkaProfile(selectedId);
              setSelectedId("");
              await refreshProfiles();
            }}
          >
            Delete
          </button>
        ) : null}
      </div>

      <div className="kafka-connection-status" aria-label="kafka-connection-status">
        <p>
          Status:{" "}
          {connectionStatus.connected
            ? `connected (${selectedProfile?.name ?? connectionStatus.profileId ?? "profile"})`
            : "not connected"}
        </p>
      </div>

      {connectionStatus.connected ? (
        <>
          <div className="kafka-topics">
            <h3>Topics</h3>
            <button type="button" onClick={() => void refreshConnectedData()}>
              Refresh topics
            </button>
            <label>
              Topic
              <select
                aria-label="kafka-topic-select"
                value={selectedTopic}
                onChange={(e) => setSelectedTopic(e.target.value)}
              >
                <option value="">— select —</option>
                {topics.map((t) => (
                  <option key={t.name} value={t.name}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
            {topicDetail ? (
              <div aria-label="kafka-topic-detail">
                <p>
                  Replication factor: {topicDetail.replicationFactor}; partitions:{" "}
                  {topicDetail.partitions.join(", ")}
                </p>
              </div>
            ) : null}
          </div>

          <div className="kafka-peek">
            <h3>Peek messages</h3>
            <label>
              Partition
              <select
                aria-label="kafka-peek-partition"
                value={peekPartition}
                onChange={(e) => setPeekPartition(Number(e.target.value))}
              >
                {(topicDetail?.partitions ?? [0]).map((id) => (
                  <option key={id} value={id}>
                    {id}
                  </option>
                ))}
              </select>
            </label>
            <input
              aria-label="kafka-peek-limit"
              type="number"
              min={1}
              max={500}
              value={peekLimit}
              onChange={(e) => setPeekLimit(Number(e.target.value))}
            />
            <input
              aria-label="kafka-peek-offset"
              type="number"
              placeholder="Offset (-1 = latest)"
              value={peekOffset}
              onChange={(e) => setPeekOffset(Number(e.target.value))}
            />
            <button type="button" onClick={onPeek} disabled={!selectedTopic}>
              Peek
            </button>
            {messages.length > 0 ? (
              <table aria-label="kafka-messages-table">
                <thead>
                  <tr>
                    <th>Offset</th>
                    <th>Key</th>
                    <th>Value</th>
                    <th>Timestamp</th>
                  </tr>
                </thead>
                <tbody>
                  {messages.map((m) => (
                    <tr key={`${m.offset}-${m.timestamp ?? 0}`}>
                      <td>{m.offset}</td>
                      <td>{m.key ?? ""}</td>
                      <td>{m.value}</td>
                      <td>{m.timestamp ?? ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : null}
          </div>

          <div className="kafka-consumer-groups">
            <h3>Consumer groups</h3>
            {consumerGroups.length === 0 ? (
              <p>No consumer groups</p>
            ) : (
              <ul aria-label="kafka-consumer-groups">
                {consumerGroups.map((g) => (
                  <li key={g.groupId}>
                    {g.groupId}
                    {g.lag !== undefined ? ` (lag: ${g.lag})` : ""}
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="kafka-publish">
            <h3>Publish</h3>
            <input
              aria-label="kafka-publish-topic"
              placeholder="Topic"
              value={publishTopic}
              onChange={(e) => setPublishTopic(e.target.value)}
            />
            <input
              aria-label="kafka-publish-key"
              placeholder="Key (optional)"
              value={publishKey}
              onChange={(e) => setPublishKey(e.target.value)}
            />
            <textarea
              aria-label="kafka-publish-payload"
              placeholder="Payload"
              value={publishPayload}
              onChange={(e) => setPublishPayload(e.target.value)}
            />
            <input
              aria-label="kafka-publish-partition"
              type="number"
              placeholder="Partition (optional)"
              value={publishPartition}
              onChange={(e) => setPublishPartition(e.target.value)}
            />
            <button
              type="button"
              onClick={onPublish}
              disabled={!publishTopic || !publishPayload}
            >
              Publish
            </button>
            {publishResult ? (
              <p aria-label="kafka-publish-result">
                Partition {publishResult.partition}, offset {publishResult.offset}
                {publishResult.timestamp !== undefined
                  ? `, timestamp ${publishResult.timestamp}`
                  : ""}
              </p>
            ) : null}
          </div>
        </>
      ) : null}

      {status ? <p className="db-status">{status}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
    </section>
  );
}
