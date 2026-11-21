// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.a2a;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.github.tomakehurst.wiremock.stubbing.ServeEvent;

import org.a2aproject.sdk.A2A;
import org.a2aproject.sdk.client.Client;
import org.a2aproject.sdk.client.ClientBuilder;
import org.a2aproject.sdk.client.ClientEvent;
import org.a2aproject.sdk.client.MessageEvent;
import org.a2aproject.sdk.client.TaskEvent;
import org.a2aproject.sdk.client.TaskUpdateEvent;
import org.a2aproject.sdk.client.config.ClientConfig;
import org.a2aproject.sdk.client.transport.jsonrpc.JSONRPCTransport;
import org.a2aproject.sdk.client.transport.jsonrpc.JSONRPCTransportConfig;
import org.a2aproject.sdk.client.transport.rest.RestTransport;
import org.a2aproject.sdk.client.transport.rest.RestTransportConfig;
import org.a2aproject.sdk.jsonrpc.common.wrappers.ListTasksResult;
import org.a2aproject.sdk.spec.A2AClientException;
import org.a2aproject.sdk.spec.A2AError;
import org.a2aproject.sdk.spec.AgentCard;
import org.a2aproject.sdk.spec.AgentInterface;
import org.a2aproject.sdk.spec.CancelTaskParams;
import org.a2aproject.sdk.spec.DataPart;
import org.a2aproject.sdk.spec.ListTasksParams;
import org.a2aproject.sdk.spec.Message;
import org.a2aproject.sdk.spec.Part;
import org.a2aproject.sdk.spec.Task;
import org.a2aproject.sdk.spec.TaskArtifactUpdateEvent;
import org.a2aproject.sdk.spec.TaskIdParams;
import org.a2aproject.sdk.spec.TaskQueryParams;
import org.a2aproject.sdk.spec.TaskState;
import org.a2aproject.sdk.spec.TaskStatusUpdateEvent;
import org.a2aproject.sdk.spec.TextPart;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import java.net.URI;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.function.Predicate;

/**
 * The official A2A Java SDK's client against the mock, over the JSON-RPC and HTTP+JSON bindings.
 * The client parses every answer strictly (ProtoJSON, unknown fields refused) into A2A 1.0's
 * types, so an answer off the contract fails the test.
 */
class A2aSdkTest extends A2aMockTest {
    /** The card as the SDK resolves it from the mock, with its interfaces at this WireMock's port. */
    private AgentCard card() throws Exception {
        AgentCard card = A2A.getAgentCard(url());
        List<AgentInterface> here = card.supportedInterfaces().stream()
                .map(i -> new AgentInterface(i.protocolBinding(), url() + URI.create(i.url()).getPath(), i.tenant(), i.protocolVersion()))
                .toList();
        return AgentCard.builder(card).supportedInterfaces(here).build();
    }

    private Client client(String binding, boolean streaming, boolean polling) throws Exception {
        ClientBuilder b = Client.builder(card())
                .clientConfig(new ClientConfig.Builder().setStreaming(streaming).setPolling(polling).build());
        if (binding.equals("JSONRPC")) {
            b.withTransport(JSONRPCTransport.class, new JSONRPCTransportConfig());
        } else {
            b.withTransport(RestTransport.class, new RestTransportConfig());
        }
        return b.build();
    }

    /** Sends a message and returns the events the client gets, once one matches until (or times out). */
    private static List<ClientEvent> send(Client client, Message message, Predicate<ClientEvent> until) throws Exception {
        List<ClientEvent> events = new CopyOnWriteArrayList<>();
        List<Throwable> errors = new CopyOnWriteArrayList<>();
        CountDownLatch done = new CountDownLatch(1);
        client.sendMessage(message, List.of((e, card) -> {
            events.add(e);
            if (until.test(e)) {
                done.countDown();
            }
        }), e -> {
            if (e != null) {
                errors.add(e);
                done.countDown();
            }
        }, null);
        assertThat(done.await(10, TimeUnit.SECONDS)).as("the events: " + events).isTrue();
        assertThat(errors).isEmpty();
        return events;
    }

    private static Task task(List<ClientEvent> events) {
        assertThat(events).singleElement().isInstanceOf(TaskEvent.class);
        return ((TaskEvent) events.get(0)).getTask();
    }

    private static String text(Message m) {
        return m.parts().stream().filter(p -> p instanceof TextPart).map(p -> ((TextPart) p).text()).reduce("", String::concat);
    }

    private static int code(Throwable e) {
        Throwable c = e;
        while (c != null && !(c instanceof A2AError)) {
            c = c.getCause();
        }
        assertThat(c).as("an A2A error in " + e).isNotNull();
        return ((A2AError) c).getCode();
    }

    @Test
    void theCardIsServedForTheSdkToResolve() throws Exception {
        AgentCard card = A2A.getAgentCard(url());
        assertThat(card.name()).isEqualTo("Partner carrier agent");
        assertThat(card.capabilities().streaming()).isTrue();
        assertThat(card.supportedInterfaces()).extracting(AgentInterface::protocolBinding).containsExactly("JSONRPC", "HTTP+JSON", "GRPC");
        assertThat(card.skills()).extracting(s -> s.id()).containsExactly("shipment-status", "pickup-booking");
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void aTaskCompletesWithArtifacts(String binding) throws Exception {
        answerAbout("PX-A2A-9001", "{\"state\": \"completed\", \"message\": \"PX-A2A-9001 is at the Leipzig depot.\", \"artifacts\": ["
                + "{\"name\": \"status\", \"description\": \"The shipment's status\", \"data\": {\"reference\": \"PX-A2A-9001\", \"status\": \"IN_TRANSIT\"}},"
                + "{\"name\": \"summary\", \"text\": \"In transit, at the Leipzig depot since 07:30.\", \"mediaType\": \"text/plain\"}]}");
        Task task = task(send(client(binding, false, false), A2A.toUserMessage("Where is PX-A2A-9001?"), e -> true));
        assertThat(task.status().state()).isEqualTo(TaskState.TASK_STATE_COMPLETED);
        assertThat(text(task.status().message())).isEqualTo("PX-A2A-9001 is at the Leipzig depot.");
        assertThat(task.status().message().role()).isEqualTo(Message.Role.ROLE_AGENT);
        assertThat(task.artifacts()).hasSize(2);
        assertThat(task.artifacts().get(0).name()).isEqualTo("status");
        assertThat(task.artifacts().get(0).parts()).singleElement().isInstanceOfSatisfying(DataPart.class,
                p -> assertThat(p.data()).isEqualTo(Map.of("reference", "PX-A2A-9001", "status", "IN_TRANSIT")));
        assertThat(task.artifacts().get(1).parts()).singleElement().isInstanceOfSatisfying(TextPart.class,
                p -> assertThat(p.text()).isEqualTo("In transit, at the Leipzig depot since 07:30."));
        assertThat(task.history()).extracting(Message::role).containsExactly(Message.Role.ROLE_USER, Message.Role.ROLE_AGENT);
        assertThat(task.contextId()).isNotBlank();
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void aTaskAsksForInputAndTheNextMessageCompletesIt(String binding) throws Exception {
        answer("[{\"matchesJsonPath\": {\"expression\": \"$.text\", \"contains\": \"PX-A2A-9002\"}}, {\"matchesJsonPath\": \"$[?(@.task == null)]\"}]",
                "{\"state\": \"input-required\", \"message\": \"Which day should the carrier pick up PX-A2A-9002?\"}");
        answer("[{\"matchesJsonPath\": \"$.task[?(@.state == 'input-required')]\"}, {\"matchesJsonPath\": {\"expression\": \"$.conversation\", \"contains\": \"PX-A2A-9002\"}},"
                + " {\"matchesJsonPath\": {\"expression\": \"$.text\", \"contains\": \"Friday\"}}]",
                "{\"state\": \"completed\", \"message\": \"Pickup of PX-A2A-9002 booked for Friday.\", \"artifacts\": [{\"name\": \"booking\", \"data\": {\"pickupId\": \"PU-3301\"}}]}");
        Client client = client(binding, false, false);
        Task first = task(send(client, A2A.toUserMessage("Book a pickup of PX-A2A-9002 at maple-crafts"), e -> true));
        assertThat(first.status().state()).isEqualTo(TaskState.TASK_STATE_INPUT_REQUIRED);
        assertThat(text(first.status().message())).contains("Which day");

        Task second = task(send(client, A2A.createUserTextMessage("Friday, please", first.contextId(), first.id()), e -> true));
        assertThat(second.id()).isEqualTo(first.id());
        assertThat(second.contextId()).isEqualTo(first.contextId());
        assertThat(second.status().state()).isEqualTo(TaskState.TASK_STATE_COMPLETED);
        assertThat(second.artifacts()).singleElement().satisfies(a -> assertThat(a.name()).isEqualTo("booking"));
        assertThat(second.history()).hasSize(4);

        // A task that has ended takes no more messages.
        assertThatThrownBy(() -> send(client, A2A.createUserTextMessage("And Monday?", first.contextId(), first.id()), e -> true))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32004));
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void theAgentCanReplyWithAMessage(String binding) throws Exception {
        answerAbout("opening hours", "{\"reply\": \"The Leipzig depot is open 07:00-19:00.\", \"data\": {\"depot\": \"LEJ\", \"opens\": \"07:00\", \"closes\": \"19:00\"}}");
        List<ClientEvent> events = send(client(binding, false, false), A2A.toUserMessage("What are the Leipzig depot's opening hours?"), e -> true);
        assertThat(events).singleElement().isInstanceOf(MessageEvent.class);
        Message reply = ((MessageEvent) events.get(0)).getMessage();
        assertThat(reply.role()).isEqualTo(Message.Role.ROLE_AGENT);
        List<Part<?>> parts = reply.parts();
        assertThat(parts).hasSize(2);
        assertThat(((TextPart) parts.get(0)).text()).isEqualTo("The Leipzig depot is open 07:00-19:00.");
        assertThat(((DataPart) parts.get(1)).data()).isEqualTo(Map.of("depot", "LEJ", "opens", "07:00", "closes", "19:00"));
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void aStreamSendsTheTaskItsUpdatesItsArtifactsAndItsEnd(String binding) throws Exception {
        answerAbout("PX-A2A-9004", "{\"state\": \"completed\", \"message\": \"PX-A2A-9004 was delivered.\","
                + " \"updates\": [\"Looking up PX-A2A-9004\", \"Asking the Leipzig depot\"],"
                + " \"artifacts\": [{\"name\": \"proof-of-delivery\", \"url\": \"https://carrier.example/pod/PX-A2A-9004.pdf\", \"mediaType\": \"application/pdf\"}]}");
        List<ClientEvent> events = send(client(binding, true, false), A2A.toUserMessage("Was PX-A2A-9004 delivered?"),
                e -> e instanceof TaskUpdateEvent u && u.getUpdateEvent() instanceof TaskStatusUpdateEvent s && s.isFinal());
        assertThat(events).hasSize(5);
        assertThat(events.get(0)).isInstanceOfSatisfying(TaskEvent.class, e -> assertThat(e.getTask().status().state()).isEqualTo(TaskState.TASK_STATE_SUBMITTED));
        for (int i = 1; i <= 2; i++) {
            assertThat(events.get(i)).isInstanceOfSatisfying(TaskUpdateEvent.class, e -> assertThat(e.getUpdateEvent())
                    .isInstanceOfSatisfying(TaskStatusUpdateEvent.class, s -> assertThat(s.status().state()).isEqualTo(TaskState.TASK_STATE_WORKING)));
        }
        assertThat(text(((TaskStatusUpdateEvent) ((TaskUpdateEvent) events.get(1)).getUpdateEvent()).status().message())).isEqualTo("Looking up PX-A2A-9004");
        assertThat(events.get(3)).isInstanceOfSatisfying(TaskUpdateEvent.class, e -> assertThat(e.getUpdateEvent())
                .isInstanceOfSatisfying(TaskArtifactUpdateEvent.class, a -> {
                    assertThat(a.artifact().name()).isEqualTo("proof-of-delivery");
                    assertThat(a.lastChunk()).isTrue();
                }));
        TaskUpdateEvent last = (TaskUpdateEvent) events.get(4);
        assertThat(((TaskStatusUpdateEvent) last.getUpdateEvent()).status().state()).isEqualTo(TaskState.TASK_STATE_COMPLETED);
        assertThat(last.getTask().artifacts()).hasSize(1);
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void aStreamedReplyIsOneMessage(String binding) throws Exception {
        answerAbout("hello", "{\"reply\": \"Hello from the partner carrier.\"}");
        List<ClientEvent> events = send(client(binding, true, false), A2A.toUserMessage("hello"), e -> e instanceof MessageEvent);
        assertThat(events).singleElement().isInstanceOf(MessageEvent.class);
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void tasksAreRememberedListedAndCanceled(String binding) throws Exception {
        answerAbout("PX-A2A-9005", "{\"state\": \"completed\", \"message\": \"Delivered.\"}");
        answerAbout("PX-A2A-9006", "{\"state\": \"working\", \"message\": \"Waiting for the depot's scan.\"}");
        // Answered at once: working, and the stub's state when it is next looked at.
        Client polling = client(binding, false, true);
        Task quick = task(send(polling, A2A.toUserMessage("Where is PX-A2A-9005?"), e -> true));
        assertThat(quick.status().state()).isEqualTo(TaskState.TASK_STATE_WORKING);
        Client client = client(binding, false, false);
        Task got = client.getTask(TaskQueryParams.builder().id(quick.id()).build(), null);
        assertThat(got.status().state()).isEqualTo(TaskState.TASK_STATE_COMPLETED);
        assertThat(text(got.status().message())).isEqualTo("Delivered.");

        Task working = task(send(client, A2A.createUserTextMessage("Is PX-A2A-9006 scanned yet?", quick.contextId(), null), e -> true));
        assertThat(working.status().state()).isEqualTo(TaskState.TASK_STATE_WORKING);
        // The SDK's JSON-RPC transport needs a tenant (an empty one: none) to list tasks.
        ListTasksResult list = client.listTasks(ListTasksParams.builder().contextId(quick.contextId()).tenant("").build(), null);
        assertThat(list.tasks()).extracting(Task::id).containsExactly(working.id(), quick.id());
        assertThat(list.totalSize()).isEqualTo(2);

        Task canceled = client.cancelTask(CancelTaskParams.builder().id(working.id()).build(), null);
        assertThat(canceled.status().state()).isEqualTo(TaskState.TASK_STATE_CANCELED);
        assertThatThrownBy(() -> client.cancelTask(CancelTaskParams.builder().id(working.id()).build(), null))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32002));
        assertThatThrownBy(() -> client.getTask(TaskQueryParams.builder().id("no-such-task").build(), null))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32001));
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void aSubscriptionSendsTheTaskThenHowItEnds(String binding) throws Exception {
        answerAbout("PX-A2A-9007", "{\"state\": \"completed\", \"message\": \"PX-A2A-9007 was delivered.\", \"artifacts\": [{\"name\": \"summary\", \"text\": \"Delivered at 10:12.\"}]}");
        Task quick = task(send(client(binding, false, true), A2A.toUserMessage("Track PX-A2A-9007"), e -> true));
        List<ClientEvent> events = new CopyOnWriteArrayList<>();
        CountDownLatch done = new CountDownLatch(1);
        client(binding, true, false).subscribeToTask(TaskIdParams.builder().id(quick.id()).build(), List.of((e, c) -> {
            events.add(e);
            if (e instanceof TaskUpdateEvent u && u.getUpdateEvent() instanceof TaskStatusUpdateEvent s && s.isFinal()) {
                done.countDown();
            }
        }), null, null);
        assertThat(done.await(10, TimeUnit.SECONDS)).as("the events: " + events).isTrue();
        assertThat(events).hasSize(3);
        assertThat(((TaskEvent) events.get(0)).getTask().status().state()).isEqualTo(TaskState.TASK_STATE_WORKING);
        assertThat(((TaskUpdateEvent) events.get(1)).getUpdateEvent()).isInstanceOf(TaskArtifactUpdateEvent.class);
        assertThat(((TaskUpdateEvent) events.get(2)).getTask().status().state()).isEqualTo(TaskState.TASK_STATE_COMPLETED);
    }

    @ParameterizedTest
    @ValueSource(strings = {"JSONRPC", "HTTP+JSON"})
    void errorsAreTheProtocolsErrors(String binding) throws Exception {
        Client client = client(binding, false, false);
        // No stub answers: a setup problem the answer names.
        assertThatThrownBy(() -> send(client, A2A.toUserMessage("Where is PX-A2A-9008?"), e -> true))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32603))
                .satisfies(e -> assertThat(e.toString() + e.getCause()).contains("no stub answers the message: Where is PX-A2A-9008?"));
        // A stub's error is the agent's.
        answerAbout("label for PX-A2A-9009", "{\"error\": {\"code\": -32005, \"message\": \"The agent sends labels as application/pdf only.\"}}");
        assertThatThrownBy(() -> send(client, A2A.toUserMessage("Send the label for PX-A2A-9009 as PNG"), e -> true))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32005));
        // A message to a task that does not exist.
        assertThatThrownBy(() -> send(client, A2A.createUserTextMessage("Friday", null, "no-such-task"), e -> true))
                .satisfies(e -> assertThat(code(e)).isEqualTo(-32001));
        assertThat(served()).noneMatch(e -> e.getRequest().getUrl().startsWith("/a2a/messages"));
        assertThatThrownBy(() -> client.getExtendedAgentCard(null, null)).isInstanceOf(A2AClientException.class);
    }

    @Test
    @SuppressWarnings("unchecked")
    void theJournalRecordsTheMessagesTheAgentReceived() throws Exception {
        answerAbout("PX-A2A-9010", "{\"state\": \"completed\"}");
        send(client("JSONRPC", false, false), A2A.toUserMessage("Where is PX-A2A-9010?"), e -> true);
        send(client("HTTP+JSON", true, false), A2A.toUserMessage("And PX-A2A-9010 again?"),
                e -> e instanceof TaskUpdateEvent u && u.getUpdateEvent() instanceof TaskStatusUpdateEvent s && s.isFinal());
        List<ServeEvent> posts = served().stream().filter(e -> e.getRequest().getMethod().getName().equals("POST")).toList();
        assertThat(posts).extracting(e -> e.getRequest().getUrl()).containsExactly(JSONRPC, REST + "/message:stream");
        Map<String, Object> rpc = json(posts.get(0).getRequest().getBodyAsString());
        assertThat(rpc).containsEntry("method", "SendMessage");
        Map<String, Object> message = (Map<String, Object>) ((Map<String, Object>) rpc.get("params")).get("message");
        assertThat((List<Map<String, Object>>) message.get("parts")).extracting(p -> p.get("text")).containsExactly("Where is PX-A2A-9010?");
        Map<String, Object> rest = json(posts.get(1).getRequest().getBodyAsString());
        assertThat((List<Map<String, Object>>) ((Map<String, Object>) rest.get("message")).get("parts")).extracting(p -> p.get("text"))
                .containsExactly("And PX-A2A-9010 again?");
    }
}
