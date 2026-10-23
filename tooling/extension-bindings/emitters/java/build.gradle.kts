plugins {
    application
}

group = "io.runtimeconditions.bindings"
version = "0.1.0"

repositories {
    mavenCentral()
}

dependencyLocking {
    lockAllConfigurations()
}

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}

dependencies {
    implementation("org.snakeyaml:snakeyaml-engine:3.2")
    implementation("com.networknt:json-schema-validator:1.5.9")
    implementation("com.fasterxml.jackson.core:jackson-databind:2.22.4")
    runtimeOnly("org.slf4j:slf4j-nop:2.0.17")

    testImplementation(platform("org.junit:junit-bom:6.1.3"))
    testImplementation("org.junit.jupiter:junit-jupiter")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}

application {
    applicationName = "runtimeconditions-binding-emitter-java"
    mainClass = "io.runtimeconditions.bindings.emitter.Main"
}

tasks.processResources {
    filesMatching("emitter.properties") {
        expand("name" to application.applicationName, "version" to project.version)
    }
    from(layout.projectDirectory.dir("templates")) {
        into("templates")
    }
    from(layout.projectDirectory.file("package-target.schema.yaml")) {
        into("target")
    }
    from(layout.projectDirectory.file("../../model/runtimeconditions.binding-model.schema.yaml")) {
        into("model")
    }
}

tasks.test {
    useJUnitPlatform()
    systemProperty("rc.emitter.root", projectDir.absolutePath)
    systemProperty("rc.gradle.version", gradle.gradleVersion)
    maxHeapSize = "1g"
}
