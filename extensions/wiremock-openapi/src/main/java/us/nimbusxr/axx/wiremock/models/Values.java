// SPDX-License-Identifier: Apache-2.0
package us.nimbusxr.axx.wiremock.models;

import com.github.tomakehurst.wiremock.common.Json;

import us.nimbusxr.axx.wiremock.json.JsonText;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** JSON values as maps and lists, and the text in them. */
final class Values {
    private Values() {}

    /** An object of keys and values, in their order, nulls kept. */
    static Map<String, Object> obj(Object... kv) {
        if (kv.length % 2 != 0) {
            throw new IllegalArgumentException("an object is keys and values, in pairs: " + kv.length + " is odd");
        }
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i < kv.length; i += 2) {
            m.put((String) kv[i], kv[i + 1]);
        }
        return m;
    }

    static List<Object> arr(Object... items) {
        return new ArrayList<>(List.of(items));
    }

    /** Parses JSON text; null when it is not JSON. */
    static Object parse(String text) {
        if (text == null || text.isBlank()) {
            return null;
        }
        try {
            return Json.read(text, Object.class);
        } catch (RuntimeException e) {
            return null;
        }
    }

    static String write(Object v) {
        return JsonText.write(v);
    }

    @SuppressWarnings("unchecked")
    static Map<String, Object> map(Object v) {
        return v instanceof Map<?, ?> m ? (Map<String, Object>) m : Map.of();
    }

    static List<Object> list(Object v) {
        return v instanceof List<?> l ? new ArrayList<>(l) : List.of();
    }

    static String string(Object v) {
        return v instanceof String s ? s : null;
    }

    /** A value at a path of keys, or null. */
    static Object at(Object v, String... path) {
        for (String k : path) {
            if (!(v instanceof Map<?, ?> m)) {
                return null;
            }
            v = m.get(k);
        }
        return v;
    }

    /**
     * The text of a message's content: a string, or the text of its parts ({@code {"text": ...}},
     * {@code {"type": "refusal", "refusal": ...}}), one per line.
     */
    static String text(Object content) {
        if (content == null) {
            return "";
        }
        if (content instanceof String s) {
            return s;
        }
        List<String> out = new ArrayList<>();
        for (Object part : content instanceof List<?> l ? l : List.of(content)) {
            if (part instanceof String s) {
                out.add(s);
            } else if (part instanceof Map<?, ?> m) {
                for (String k : List.of("text", "refusal", "thinking")) {
                    if (m.get(k) instanceof String s) {
                        out.add(s);
                    }
                }
                if (m.get("json") != null) {
                    out.add(write(m.get("json")));
                }
            }
        }
        return String.join("\n", out);
    }

    /**
     * Text to search: the text, and when it is JSON, the strings in it as well. A service that
     * sends a tool's result as JSON text may have escaped what it holds ({@code
     * "Hauptstraße"}), which a search of the text alone would miss.
     */
    static String searchable(String text) {
        String t = text.strip();
        if (!(t.startsWith("{") || t.startsWith("["))) {
            return text;
        }
        Object v = parse(t);
        if (v == null) {
            return text;
        }
        List<String> strings = new ArrayList<>();
        strings(v, strings);
        return text + "\n" + String.join("\n", strings);
    }

    /** Every string in a JSON value, with the strings in strings that are JSON themselves. */
    static void strings(Object v, List<String> out) {
        if (v instanceof String s) {
            out.add(searchable(s));
        } else if (v instanceof Map<?, ?> m) {
            for (Object e : m.values()) {
                strings(e, out);
            }
        } else if (v instanceof List<?> l) {
            for (Object e : l) {
                strings(e, out);
            }
        } else if (v != null) {
            out.add(String.valueOf(v));
        }
    }

    /**
     * Text in the pieces a stream sends it in: words with the spaces after them, so the pieces
     * join back to the text.
     */
    static List<String> words(String text) {
        List<String> out = new ArrayList<>();
        int start = 0;
        for (int i = 0; i < text.length(); i++) {
            if (Character.isWhitespace(text.charAt(i)) && (i + 1 == text.length() || !Character.isWhitespace(text.charAt(i + 1)))) {
                out.add(text.substring(start, i + 1));
                start = i + 1;
            }
        }
        if (start < text.length()) {
            out.add(text.substring(start));
        }
        return out;
    }

    /** JSON text in the pieces a stream sends it in: at most 16 characters each. */
    static List<String> pieces(String text) {
        List<String> out = new ArrayList<>();
        for (int i = 0; i < text.length(); i += 16) {
            out.add(text.substring(i, Math.min(text.length(), i + 16)));
        }
        return out;
    }

    /** The first n hex digits of the SHA-256 of the parts, as ids made from what they identify. */
    static String hash(int n, Object... parts) {
        try {
            MessageDigest d = MessageDigest.getInstance("SHA-256");
            for (Object p : parts) {
                d.update(String.valueOf(p).getBytes(StandardCharsets.UTF_8));
                d.update((byte) 0);
            }
            return HexFormat.of().formatHex(d.digest()).substring(0, n);
        } catch (NoSuchAlgorithmException e) {
            throw new IllegalStateException(e);
        }
    }

    /** A rough token count: a token is about four characters of text. */
    static int tokens(String text) {
        return text == null || text.isEmpty() ? 0 : Math.max(1, (text.length() + 3) / 4);
    }
}
