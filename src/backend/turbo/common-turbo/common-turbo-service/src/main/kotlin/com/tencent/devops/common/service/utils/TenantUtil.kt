package com.tencent.devops.common.service.utils

import org.slf4j.LoggerFactory
import org.springframework.beans.factory.InitializingBean
import org.springframework.context.ApplicationContext
import org.springframework.context.ApplicationContextAware
import org.springframework.core.env.get

class TenantUtil : ApplicationContextAware, InitializingBean {

    private var applicationContext: ApplicationContext? = null

    override fun setApplicationContext(applicationContext: ApplicationContext) {
        this.applicationContext = applicationContext
    }

    override fun afterPropertiesSet() {
        val environment = applicationContext?.environment ?: return

        enableMultiTenantMode = environment["bk.enableMultiTenantMode"] == "true"
    }

    companion object {
        private var enableMultiTenantMode: Boolean = false
        private const val DEFAULT_TENANT_ID_FOR_SINGLE = "default"
        public const val DEFAULT_TENANT_ID_FOR_MULTI = "system"
        private val logger = LoggerFactory.getLogger(this::class.java)

        /**
         * 是否开启多租户模式
         */
        fun isMultiTenantMode(): Boolean = enableMultiTenantMode

        /**
         * 获取租户id
         */
        fun getTenantId(tenantId: String? = null): String? {
            return when {
                !enableMultiTenantMode -> null
                !tenantId.isNullOrBlank() -> tenantId
                else -> DEFAULT_TENANT_ID_FOR_MULTI
            }
        }

        /**
         * 生成英文名称
         */
        fun parseEnglishName(tenantId: String? = null, tenantEnglishName: String): String {
            return when {
                tenantEnglishName.contains(".") -> tenantEnglishName
                !enableMultiTenantMode -> tenantEnglishName
                !tenantId.isNullOrBlank() -> "$tenantId.$tenantEnglishName"
                else -> tenantEnglishName
            }
        }

        /**
         * 根据英文名称获取租户id
         */
        fun getTenantIdByEnglishName(tenantEnglishName: String): String? {
            return when {
                tenantEnglishName.contains(".") -> tenantEnglishName.substringBefore(".")
                enableMultiTenantMode -> DEFAULT_TENANT_ID_FOR_MULTI
                else -> null
            }
        }
    }
}
