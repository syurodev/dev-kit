import { callBridge, callBridgeCancellable, type CancellableCall } from "./client";
import { AppService } from "../../bindings/devkit/internal/bridge";

export type KafkaProfile = {
  id: string;
  name: string;
  bootstrapServers: string[];
  securityProtocol: string;
  saslMechanism: string;
  username: string;
  env: string;
};

export type CreateKafkaProfileInput = {
  name: string;
  bootstrapServers: string[];
  securityProtocol: string;
  saslMechanism: string;
  username: string;
  secret: string;
  env: string;
};

export type KafkaConnectionStatus = {
  connected: boolean;
  profileId?: string;
};

export type KafkaTopic = {
  name: string;
};

export type KafkaTopicDetail = {
  name: string;
  replicationFactor: number;
  partitions: number[];
};

export type KafkaMessage = {
  offset: number;
  key?: string;
  value: string;
  timestamp?: number;
};

export type KafkaConsumerGroup = {
  groupId: string;
  lag?: number;
};

export type PublishInput = {
  topic: string;
  key?: string;
  value: string;
  partition?: number;
};

export type PublishOutcome = {
  partition: number;
  offset: number;
  timestamp?: number;
};

function toProfile(p: {
  id: string;
  name: string;
  bootstrap_servers: string[] | null;
  security_protocol: string;
  sasl_mechanism: string;
  username: string;
  env: string;
}): KafkaProfile {
  return {
    id: p.id,
    name: p.name,
    bootstrapServers: p.bootstrap_servers ?? [],
    securityProtocol: p.security_protocol,
    saslMechanism: p.sasl_mechanism,
    username: p.username,
    env: p.env,
  };
}

function toConnectionStatus(s: {
  connected: boolean;
  profile_id?: string;
}): KafkaConnectionStatus {
  return {
    connected: s.connected,
    profileId: s.profile_id,
  };
}

function toTopicDetail(d: {
  name: string;
  replication_factor: number;
  partitions: { id: number }[] | null;
}): KafkaTopicDetail {
  return {
    name: d.name,
    replicationFactor: d.replication_factor,
    partitions: (d.partitions ?? []).map((p) => p.id),
  };
}

function toMessage(m: {
  offset: number;
  key?: string;
  value: string;
  timestamp?: number;
}): KafkaMessage {
  return {
    offset: m.offset,
    key: m.key,
    value: m.value,
    timestamp: m.timestamp,
  };
}

function toConsumerGroup(g: { group_id: string; lag?: number }): KafkaConsumerGroup {
  return {
    groupId: g.group_id,
    lag: g.lag,
  };
}

function toPublishOutcome(r: {
  partition: number;
  offset: number;
  timestamp?: number;
}): PublishOutcome {
  return {
    partition: r.partition,
    offset: r.offset,
    timestamp: r.timestamp,
  };
}

export function listKafkaProfiles(): Promise<KafkaProfile[]> {
  return callBridge(async () => {
    const list = await AppService.KafkaListProfiles();
    return (list ?? []).map(toProfile);
  });
}

export function createKafkaProfile(input: CreateKafkaProfileInput): Promise<KafkaProfile> {
  return callBridge(async () => {
    const created = await AppService.KafkaCreateProfile({
      name: input.name,
      bootstrap_servers: input.bootstrapServers,
      security_protocol: input.securityProtocol,
      sasl_mechanism: input.saslMechanism,
      username: input.username,
      secret: input.secret,
      env: input.env,
    });
    return toProfile(created);
  });
}

export function deleteKafkaProfile(id: string): Promise<void> {
  return callBridge(() => AppService.KafkaDeleteProfile(id) as Promise<void>);
}

export function testKafkaConnection(id: string): Promise<void> {
  return callBridge(() => AppService.KafkaTestConnection(id) as Promise<void>);
}

export function connectKafka(id: string): Promise<void> {
  return callBridge(() => AppService.KafkaConnect(id) as Promise<void>);
}

export function disconnectKafka(): Promise<void> {
  return callBridge(() => AppService.KafkaDisconnect() as Promise<void>);
}

export function getKafkaConnectionStatus(): Promise<KafkaConnectionStatus> {
  return callBridge(async () => {
    const status = await AppService.KafkaConnectionStatus();
    return toConnectionStatus(status);
  });
}

export function listKafkaTopics(): Promise<KafkaTopic[]> {
  return callBridge(async () => {
    const list = await AppService.KafkaListTopics();
    return (list ?? []).map((t) => ({ name: t.name }));
  });
}

export function describeKafkaTopic(topic: string): Promise<KafkaTopicDetail> {
  return callBridge(async () => {
    const detail = await AppService.KafkaDescribeTopic(topic);
    return toTopicDetail(detail);
  });
}

/**
 * Cancellable: a peek can wait on a broker across a network, which is the one
 * Kafka read slow enough to want a Cancel (see callBridgeCancellable).
 */
export function peekKafkaMessages(
  topic: string,
  partition: number,
  limit: number,
  offset: number,
): CancellableCall<KafkaMessage[]> {
  return callBridgeCancellable(
    () => AppService.KafkaPeekMessages(topic, partition, limit, offset),
    (messages) => (messages ?? []).map(toMessage),
  );
}

export function listKafkaConsumerGroups(): Promise<KafkaConsumerGroup[]> {
  return callBridge(async () => {
    const list = await AppService.KafkaListConsumerGroups();
    return (list ?? []).map(toConsumerGroup);
  });
}

export function publishKafkaMessage(input: PublishInput): Promise<PublishOutcome> {
  return callBridge(async () => {
    const result = await AppService.KafkaPublishMessage({
      topic: input.topic,
      key: input.key,
      value: input.value,
      ...(typeof input.partition === "number" ? { partition: input.partition } : {}),
    });
    return toPublishOutcome(result);
  });
}
