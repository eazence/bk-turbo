import com.tencent.devops.utils.findPropertyOrEmpty
plugins {
	id("com.tencent.devops.boot") version "0.0.7"
	id("org.owasp.dependencycheck") version "7.1.0.1"
}

allprojects {
    group = "com.tencent.bk.devops.turbo"

    version = (System.getProperty("turbo_version") ?: "0.0.2") +
        if (System.getProperty("snapshot") == "true") "-SNAPSHOT" else "-RELEASE"

    apply(plugin = "com.tencent.devops.boot")

    val property = project.findPropertyOrEmpty("devops.assemblyMode").trim()

    repositories {
        maven(url = "https://mirrors.tencent.com/repository/maven/bkdevops_maven")
    }

    configurations.forEach {
        it.exclude(group = "org.slf4j", module = "log4j-over-slf4j")
        it.exclude(group = "org.slf4j", module = "slf4j-log4j12")
        it.exclude(group = "org.slf4j", module = "slf4j-nop")
        if (project.name.startsWith("boot-")) {
            when (com.tencent.devops.enums.AssemblyMode.ofValueOrDefault(property)) {
                com.tencent.devops.enums.AssemblyMode.CONSUL -> {
                    it.exclude("org.springframework.cloud", "spring-cloud-starter-kubernetes-client")
                    it.exclude("org.springframework.cloud", "spring-cloud-starter-kubernetes-client-config")
                }
                com.tencent.devops.enums.AssemblyMode.K8S, com.tencent.devops.enums.AssemblyMode.KUBERNETES -> {
                    it.exclude("org.springframework.cloud", "spring-cloud-starter-config")
                    it.exclude("org.springframework.cloud", "spring-cloud-starter-consul-config")
                    it.exclude("org.springframework.cloud", "spring-cloud-starter-consul-discovery")
                }
            }
        }
        it.resolutionStrategy.cacheChangingModulesFor(0, TimeUnit.MINUTES)
    }

	dependencyManagement {
		dependencies {
			dependency("javax.ws.rs:javax.ws.rs-api:${Versions.jaxrsVersion}")
			dependency("com.github.ulisesbocchio:jasypt-spring-boot-starter:${Versions.jasyptVersion}")
			dependency("org.bouncycastle:bcprov-jdk15on:${Versions.bouncyCastleVersion}")
			dependency("com.google.guava:guava:${Versions.guavaVersion}")
			dependency("io.jsonwebtoken:jjwt:${Versions.jjwtVersion}")
			dependency("commons-io:commons-io:${Versions.commonIo}")
			dependencySet("io.swagger:${Versions.swaggerVersion}") {
                entry("swagger-annotations")
                entry("swagger-jersey2-jaxrs")
                entry("swagger-models")
            }
            dependencySet("io.micrometer:${Versions.micrometerVersion}") {
                entry("micrometer-registry-prometheus")
            }
            dependency("com.squareup.okhttp3:okhttp:${Versions.okHttpVersion}")
		}
	}
}
