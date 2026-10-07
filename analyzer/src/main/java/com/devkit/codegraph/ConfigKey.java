package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.List;

/**
 * One property a config file sets: {@code fsap.access.module-based-enabled: ${FSAP_ACCESS_MODULE_BASED_ENABLED:false}}
 * in {@code application.yaml}, flattened to a dotted key.
 *
 * The value is what the file says, never what the running service will see:
 * profiles, environment variables and command-line overrides all sit above it.
 * A value whose key names a secret is not kept ({@code redacted}) — this index
 * is read by agents, and a password in a config file is not theirs to read.
 */
public class ConfigKey {
    /** The dotted key, list entries as {@code a.b[0]}. */
    public String key;
    /** Path relative to the project root. */
    public String file;
    public int line;
    /** The value as written, clipped; "" when {@link #redacted}. */
    public String value = "";
    public boolean redacted;
    /** The profile the entry applies to: from {@code application-prod.yml} or the document's on-profile; "" for every profile. */
    public String profile = "";
    /** Environment variables the value is built from — {@code ${NAME:default}} with an env-style NAME. */
    public List<String> envVars = new ArrayList<>();

    public ConfigKey() {}
}
