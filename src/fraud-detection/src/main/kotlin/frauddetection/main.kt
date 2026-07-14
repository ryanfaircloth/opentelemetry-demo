/*
 * Copyright The OpenTelemetry Authors
 * SPDX-License-Identifier: Apache-2.0
 */

package frauddetection

import org.apache.kafka.clients.CommonClientConfigs
import org.apache.kafka.clients.consumer.ConsumerConfig.*
import org.apache.kafka.clients.consumer.KafkaConsumer
import org.apache.kafka.common.KafkaException
import org.apache.kafka.common.config.SaslConfigs
import org.apache.kafka.common.config.SslConfigs
import org.apache.kafka.common.serialization.ByteArrayDeserializer
import org.apache.kafka.common.serialization.StringDeserializer
import org.apache.logging.log4j.LogManager
import org.apache.logging.log4j.Logger
import oteldemo.Demo.*
import java.time.Duration.ofMillis
import java.util.*
import kotlin.system.exitProcess
import dev.openfeature.contrib.providers.flagd.FlagdOptions
import dev.openfeature.contrib.providers.flagd.FlagdProvider
import dev.openfeature.sdk.Client
import dev.openfeature.sdk.EvaluationContext
import dev.openfeature.sdk.ImmutableContext
import dev.openfeature.sdk.Value
import dev.openfeature.sdk.OpenFeatureAPI

val topic: String = System.getenv("KAFKA_TOPIC") ?: "orders"
const val groupID = "fraud-detection"

private val logger: Logger = LogManager.getLogger(groupID)

fun main() {
    val options = FlagdOptions.builder()
    .withGlobalTelemetry(true)
    .build()
    val flagdProvider = FlagdProvider(options)
    OpenFeatureAPI.getInstance().setProvider(flagdProvider)

    logger.info("Connecting to Kafka bootstrap.servers=${System.getenv("KAFKA_ADDR")}, topic=$topic, groupId=$groupID")
    val props = buildConsumerProps()
    val consumer = connectConsumerWithRetry(props)

    var totalCount = 0L

    consumer.use {
        while (true) {
            // KafkaConsumer already retries transient broker disconnects/timeouts
            // internally, but catch RetriableException here too as a fallback so a
            // rare escape doesn't crash the whole process - just log and poll again.
            val records = try {
                consumer.poll(ofMillis(100))
            } catch (e: org.apache.kafka.common.errors.RetriableException) {
                logger.warn("Retriable error polling Kafka, will retry: ${e.message}")
                continue
            }

            totalCount = records.fold(totalCount) { accumulator, record ->
                if (getFeatureFlagValue("kafkaQueueProblems") > 0) {
                    logger.info("FeatureFlag 'kafkaQueueProblems' is enabled, sleeping 1 second")
                    Thread.sleep(1000)
                }
                try {
                    val orders = OrderResult.parseFrom(record.value())
                    val newCount = accumulator + 1
                    logger.info("Consumed record with orderId: ${orders.orderId}, and updated total count to: $newCount")
                    newCount
                } catch (e: Exception) {
                    logger.error("Failed to process record at offset ${record.offset()}, skipping", e)
                    accumulator
                }
            }
        }
    }
}

// Retries the initial connection/subscription against Kafka with exponential
// backoff, since a broker that is still starting up (e.g. during a cluster
// rollout) would otherwise crash the process on the very first attempt.
// Config/programming errors (anything other than KafkaException) are not
// retried since retrying can't fix them.
fun connectConsumerWithRetry(props: Properties): KafkaConsumer<String, ByteArray> {
    val totalBudgetMillis = 120_000L
    val maxDelayMillis = 30_000L
    var delayMillis = 1_000L
    val deadline = System.currentTimeMillis() + totalBudgetMillis

    while (true) {
        try {
            return KafkaConsumer<String, ByteArray>(props).apply {
                subscribe(listOf(topic))
            }
        } catch (e: KafkaException) {
            val now = System.currentTimeMillis()
            if (now >= deadline) {
                logger.error("Failed to connect to Kafka after retrying for ${totalBudgetMillis}ms, giving up: ${e.message}", e)
                exitProcess(1)
            }
            logger.warn("Failed to connect to Kafka, retrying in ${delayMillis}ms: ${e.message}")
            Thread.sleep(delayMillis)
            delayMillis = minOf(delayMillis * 2, maxDelayMillis)
        }
    }
}

fun buildConsumerProps(): Properties {
    val props = Properties()
    props[KEY_DESERIALIZER_CLASS_CONFIG] = StringDeserializer::class.java.name
    props[VALUE_DESERIALIZER_CLASS_CONFIG] = ByteArrayDeserializer::class.java.name
    props[GROUP_ID_CONFIG] = groupID
    // Read from the start of the topic so a freshly-joined consumer group still
    // processes orders produced before it finished joining, matching the
    // accounting consumer's behaviour. Without this the Kafka default of
    // "latest" silently drops those orders, so fraud-detection may emit no
    // telemetry on a quiet/cold start.
    props[AUTO_OFFSET_RESET_CONFIG] = "earliest"
    val bootstrapServers = System.getenv("KAFKA_ADDR")
    if (bootstrapServers == null) {
        logger.error("KAFKA_ADDR is not supplied")
        exitProcess(1)
    }
    props[BOOTSTRAP_SERVERS_CONFIG] = bootstrapServers

    // SASL/TLS from the KAFKA_PROTOCOL, KAFKA_SASL_MECHANISM,
    // KAFKA_SASL_JAAS_CONFIG, and KAFKA_SSL_TRUSTSTORE_CRT env vars, matching
    // the KafkaAccess-operator secret contract
    // (https://github.com/strimzi/kafka-access-operator). Absent
    // KAFKA_PROTOCOL (or PLAINTEXT), the plaintext/no-auth behavior is unchanged.
    val securityProtocol = System.getenv("KAFKA_PROTOCOL")
    if (!securityProtocol.isNullOrEmpty() && securityProtocol != "PLAINTEXT") {
        props[CommonClientConfigs.SECURITY_PROTOCOL_CONFIG] = securityProtocol
        System.getenv("KAFKA_SASL_MECHANISM")?.let { props[SaslConfigs.SASL_MECHANISM] = it }
        System.getenv("KAFKA_SASL_JAAS_CONFIG")?.let { props[SaslConfigs.SASL_JAAS_CONFIG] = it }
        System.getenv("KAFKA_SSL_TRUSTSTORE_CRT")?.let {
            props[SslConfigs.SSL_TRUSTSTORE_TYPE_CONFIG] = "PEM"
            props[SslConfigs.SSL_TRUSTSTORE_CERTIFICATES_CONFIG] = it
        }
    }

    return props
}

/**
* Retrieves the status of a feature flag from the Feature Flag service.
*
* @param ff The name of the feature flag to retrieve.
* @return `true` if the feature flag is enabled, `false` otherwise or in case of errors.
*/
fun getFeatureFlagValue(ff: String): Int {
    val client = OpenFeatureAPI.getInstance().client
    // TODO: Plumb the actual session ID from the frontend via baggage?
    val uuid = UUID.randomUUID()

    val clientAttrs = mutableMapOf<String, Value>()
    clientAttrs["session"] = Value(uuid.toString())
    client.evaluationContext = ImmutableContext(clientAttrs)
    val intValue = client.getIntegerValue(ff, 0)
    return intValue
}
