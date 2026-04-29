-- 用户自定义 AI 配置
CREATE TABLE IF NOT EXISTS user_ai_configs (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    api_key VARCHAR(500),
    base_url VARCHAR(500),
    model VARCHAR(100) DEFAULT 'glm-4.5',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- 摘要模板（is_system=true 为内置模板，user_id=NULL）
CREATE TABLE IF NOT EXISTS summary_templates (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description VARCHAR(300),
    style VARCHAR(50) NOT NULL DEFAULT 'bullets',
    brief_prompt TEXT NOT NULL,
    detailed_prompt TEXT NOT NULL,
    is_system BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT NOW()
);

-- 系统预置模板（5种风格）
INSERT INTO summary_templates (user_id, name, description, style, brief_prompt, detailed_prompt, is_system) VALUES
(NULL, '简洁要点', '3-5个核心要点，适合快速浏览', 'bullets',
 '你是一个专业的文章摘要助手。请严格基于以下文章内容，提炼3-5个核心要点，每个要点以"• "开头，不超过30字。不要添加文章中没有的信息，不要回答与文章无关的问题。

文章标题：{title}
文章内容：
{content}

请输出要点列表：',
 '你是一个专业的文章摘要助手。请严格基于以下文章内容，生成结构化的详细摘要。只总结文章中实际提到的内容，不要扩展或联想。

文章标题：{title}
文章内容：
{content}

请输出详细摘要（300字以内）：', true),

(NULL, '深度分析', '深入分析文章观点、论据和结论', 'analysis',
 '你是一个专业的文章分析师。请基于以下文章内容（且仅基于此内容），提炼文章的核心论点，每点以"▸ "开头。

文章标题：{title}
文章内容：
{content}

核心论点：',
 '你是一个专业的文章分析师。请对以下文章进行深度分析，包括：主要论点、支撑论据、作者立场、潜在局限。所有分析必须有文章原文依据。

文章标题：{title}
文章内容：
{content}

深度分析：', true),

(NULL, '一句话速览', '极简一句话概括全文', 'oneliner',
 '用一句话（不超过50字）概括以下文章的核心内容。只基于文章内容，不添加任何额外信息。

标题：{title}
内容：{content}

一句话概括：',
 '用3句话概括以下文章：第1句说是什么，第2句说为什么重要，第3句说结论或影响。所有内容必须来自原文。

标题：{title}
内容：{content}

三句话摘要：', true),

(NULL, '轻松叙述', '像朋友讲故事一样，通俗易懂', 'casual',
 '用轻松的口吻，像跟朋友聊天一样，简单说说这篇文章讲了啥（2-3句话）。内容严格来自文章，但语气要自然随意。

文章：{title}
{content}

朋友式简介：',
 '用轻松有趣的方式介绍这篇文章的内容，让没看过的人能快速了解。用通俗语言，保持文章原意，不要夸大或歪曲。

文章：{title}
{content}

轻松解读：', true),

(NULL, '学术摘要', '正式学术风格，适合研究引用', 'academic',
 '请以正式学术摘要格式，概括以下文章的研究目的、方法、主要发现（如适用）。严格基于原文，不添加假设。

标题：{title}
内容：{content}

学术摘要：',
 '请提供以下文章的学术式综述，包含：研究背景、主要内容、核心观点、意义与局限性。所有内容必须源自原文。

标题：{title}
内容：{content}

学术综述：', true);

-- 用户默认模板偏好
ALTER TABLE users ADD COLUMN IF NOT EXISTS default_template_id INT REFERENCES summary_templates(id);

-- 文章使用的模板记录
ALTER TABLE articles ADD COLUMN IF NOT EXISTS template_id INT REFERENCES summary_templates(id);

-- 分享 token
CREATE TABLE IF NOT EXISTS share_tokens (
    id SERIAL PRIMARY KEY,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE,
    token VARCHAR(32) UNIQUE NOT NULL,
    created_by INT REFERENCES users(id),
    created_at TIMESTAMP DEFAULT NOW()
);
