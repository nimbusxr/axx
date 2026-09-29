// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.graphql;

import com.apollographql.federation.graphqljava.Federation;
import com.github.tomakehurst.wiremock.http.ResponseDefinition;

import graphql.GraphQLContext;
import graphql.GraphQLError;
import graphql.ErrorClassification;
import graphql.ErrorType;
import graphql.execution.CoercedVariables;
import graphql.execution.DataFetcherResult;
import graphql.execution.ResultPath;
import graphql.language.ArrayValue;
import graphql.language.BooleanValue;
import graphql.language.EnumValue;
import graphql.language.FloatValue;
import graphql.language.IntValue;
import graphql.language.NullValue;
import graphql.language.SourceLocation;
import graphql.language.ObjectField;
import graphql.language.ObjectValue;
import graphql.language.StringValue;
import graphql.language.Value;
import graphql.language.VariableReference;
import graphql.schema.Coercing;
import graphql.schema.DataFetcher;
import graphql.schema.DataFetchingEnvironment;
import graphql.schema.GraphQLNamedType;
import graphql.schema.GraphQLObjectType;
import graphql.schema.GraphQLScalarType;
import graphql.schema.GraphQLSchema;
import graphql.schema.TypeResolver;
import graphql.schema.idl.FieldWiringEnvironment;
import graphql.schema.idl.InterfaceWiringEnvironment;
import graphql.schema.idl.RuntimeWiring;
import graphql.schema.idl.ScalarInfo;
import graphql.schema.idl.ScalarWiringEnvironment;
import graphql.schema.idl.SchemaGenerator;
import graphql.schema.idl.SchemaParser;
import graphql.schema.idl.TypeDefinitionRegistry;
import graphql.schema.idl.UnionWiringEnvironment;
import graphql.schema.idl.WiringFactory;

import java.io.IOException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;

/** Builds the executable schema of a mock: every field answered from its stubs. */
final class StubbedSchema {
    private StubbedSchema() {}

    static GraphQLSchema build(String sdl, GraphqlMock mock) {
        TypeDefinitionRegistry registry = new SchemaParser().parse(sdl);
        RuntimeWiring wiring = RuntimeWiring.newRuntimeWiring().wiringFactory(new Wiring(mock)).build();
        if (!federated(sdl)) {
            return new SchemaGenerator().makeExecutableSchema(registry, wiring);
        }
        return Federation.transform(registry, wiring)
                .fetchEntities(env -> entities(env, mock))
                .resolveEntityType(env -> typeOf(env.getObject(), env.getSchema()))
                .build();
    }

    /** A subgraph's schema has entities, or links the federation spec. */
    private static boolean federated(String sdl) {
        return sdl.contains("@key") || sdl.contains("specs.apollo.dev/federation");
    }

    /** The entities of _entities: each representation with its stub's value over it. */
    @SuppressWarnings("unchecked")
    private static Object entities(DataFetchingEnvironment env, GraphqlMock mock) throws IOException {
        List<Map<String, Object>> representations = env.getArgument("representations");
        List<Object> out = new ArrayList<>();
        List<GraphQLError> errors = new ArrayList<>();
        ResultPath path = env.getExecutionStepInfo().getPath();
        for (int i = 0; i < representations.size(); i++) {
            Map<String, Object> rep = representations.get(i);
            Optional<ResponseDefinition> stub = mock.lookup(mock.entityPath(String.valueOf(rep.get("__typename"))), rep);
            if (stub.isEmpty()) {
                out.add(null);
                continue;
            }
            Optional<GraphqlMock.StubError> error = GraphqlMock.error(stub.get());
            if (error.isPresent()) {
                out.add(null);
                errors.add(error(env, error.get(), path.segment(i)));
                continue;
            }
            Map<String, Object> entity = new LinkedHashMap<>(rep);
            if (mock.value(stub.get()) instanceof Map<?, ?> value) {
                entity.putAll((Map<String, Object>) value);
            }
            out.add(entity);
        }
        return DataFetcherResult.newResult().data(out).errors(errors).build();
    }

    private static GraphQLError error(DataFetchingEnvironment env, GraphqlMock.StubError e, ResultPath path) {
        return new StubbedError(
                e.message(),
                env.getField().getSourceLocation(),
                (path != null ? path : env.getExecutionStepInfo().getPath()).toList(),
                e.code() == null ? Map.of() : Map.of("code", e.code()));
    }

    /** An error a stub answers: only what the stub says, no classification of graphql-java's. */
    private record StubbedError(String message, SourceLocation location, List<Object> path, Map<String, Object> ext)
            implements GraphQLError {
        @Override
        public String getMessage() {
            return message;
        }

        @Override
        public List<SourceLocation> getLocations() {
            return location == null ? List.of() : List.of(location);
        }

        @Override
        public ErrorClassification getErrorType() {
            return ErrorType.DataFetchingException;
        }

        @Override
        public List<Object> getPath() {
            return path;
        }

        @Override
        public Map<String, Object> getExtensions() {
            return ext;
        }

        @Override
        public Map<String, Object> toSpecification() {
            Map<String, Object> out = new LinkedHashMap<>();
            out.put("message", message);
            if (location != null) {
                out.put("locations", List.of(Map.of("line", location.getLine(), "column", location.getColumn())));
            }
            out.put("path", path);
            if (!ext.isEmpty()) {
                out.put("extensions", ext);
            }
            return out;
        }
    }

    /** The object type a value is: its __typename, or the only type it can be. */
    private static GraphQLObjectType typeOf(Object value, GraphQLSchema schema) {
        if (value instanceof Map<?, ?> m && m.get("__typename") instanceof String name) {
            return schema.getObjectType(name);
        }
        return null;
    }

    /** Wires every field to its stubs, every custom scalar as it comes, and abstract types by __typename. */
    private static final class Wiring implements WiringFactory {
        private final GraphqlMock mock;
        // One scalar type per name: the schema generator asks for a scalar more than once.
        private final Map<String, GraphQLScalarType> scalars = new java.util.concurrent.ConcurrentHashMap<>();

        Wiring(GraphqlMock mock) {
            this.mock = mock;
        }

        @Override
        public DataFetcher<?> getDefaultDataFetcher(FieldWiringEnvironment environment) {
            return new Fetcher(mock);
        }

        @Override
        public boolean providesScalar(ScalarWiringEnvironment environment) {
            String name = environment.getScalarTypeDefinition().getName();
            // The built-in scalars are graphql-java's, and the federation spec's the federation
            // library's.
            return !ScalarInfo.isGraphqlSpecifiedScalar(name) && !name.startsWith("_") && !name.contains("__");
        }

        @Override
        public GraphQLScalarType getScalar(ScalarWiringEnvironment environment) {
            return scalars.computeIfAbsent(
                    environment.getScalarTypeDefinition().getName(),
                    name -> GraphQLScalarType.newScalar().name(name).coercing(new AsIs()).build());
        }

        @Override
        public boolean providesTypeResolver(InterfaceWiringEnvironment environment) {
            return true;
        }

        @Override
        public TypeResolver getTypeResolver(InterfaceWiringEnvironment environment) {
            String name = environment.getInterfaceTypeDefinition().getName();
            return env -> {
                GraphQLObjectType t = typeOf(env.getObject(), env.getSchema());
                if (t != null) {
                    return t;
                }
                List<GraphQLObjectType> impls = env.getSchema().getImplementations(env.getSchema().getTypeAs(name));
                return impls.isEmpty() ? null : impls.get(0);
            };
        }

        @Override
        public boolean providesTypeResolver(UnionWiringEnvironment environment) {
            return true;
        }

        @Override
        public TypeResolver getTypeResolver(UnionWiringEnvironment environment) {
            return env -> typeOf(env.getObject(), env.getSchema());
        }
    }

    /** A field's value: its parent's, or its stub's. */
    private static final class Fetcher implements DataFetcher<Object> {
        private final GraphqlMock mock;

        Fetcher(GraphqlMock mock) {
            this.mock = mock;
        }

        @Override
        public Object get(DataFetchingEnvironment env) throws IOException {
            String field = env.getFieldDefinition().getName();
            Object source = env.getSource();
            if (source instanceof Map<?, ?> m && m.containsKey(field)) {
                return m.get(field);
            }
            if (source != null && !(source instanceof Map)) {
                return null;
            }
            String type = ((GraphQLNamedType) env.getParentType()).getName();
            Map<String, Object> body = new LinkedHashMap<>();
            body.put("arguments", env.getArguments());
            if (source != null) {
                body.put("source", source);
            }
            Optional<ResponseDefinition> stub = mock.lookup(mock.fieldPath(type, field), body);
            if (stub.isEmpty()) {
                return null;
            }
            Optional<GraphqlMock.StubError> error = GraphqlMock.error(stub.get());
            if (error.isPresent()) {
                return DataFetcherResult.newResult().error(error(env, error.get(), null)).build();
            }
            return mock.value(stub.get());
        }
    }

    /** A custom scalar: values pass as they come. */
    private static final class AsIs implements Coercing<Object, Object> {
        @Override
        public Object serialize(Object value, GraphQLContext context, Locale locale) {
            return value;
        }

        @Override
        public Object parseValue(Object input, GraphQLContext context, Locale locale) {
            return input;
        }

        @Override
        public Object parseLiteral(Value<?> input, CoercedVariables variables, GraphQLContext context, Locale locale) {
            return literal(input, variables);
        }

        private static Object literal(Value<?> v, CoercedVariables variables) {
            if (v instanceof StringValue s) {
                return s.getValue();
            } else if (v instanceof IntValue i) {
                return i.getValue();
            } else if (v instanceof FloatValue f) {
                return f.getValue();
            } else if (v instanceof BooleanValue b) {
                return b.isValue();
            } else if (v instanceof EnumValue e) {
                return e.getName();
            } else if (v instanceof NullValue) {
                return null;
            } else if (v instanceof VariableReference r) {
                return variables.get(r.getName());
            } else if (v instanceof ArrayValue a) {
                List<Object> out = new ArrayList<>();
                for (Value<?> e : a.getValues()) {
                    out.add(literal(e, variables));
                }
                return out;
            } else if (v instanceof ObjectValue o) {
                Map<String, Object> out = new LinkedHashMap<>();
                for (ObjectField f : o.getObjectFields()) {
                    out.put(f.getName(), literal(f.getValue(), variables));
                }
                return out;
            }
            return null;
        }
    }
}
