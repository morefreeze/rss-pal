CREATE TABLE IF NOT EXISTS public_feed_catalog (
 id SERIAL PRIMARY KEY,
 title VARCHAR(200) NOT NULL,
 url VARCHAR(2048) NOT NULL UNIQUE,
 category VARCHAR(80) NOT NULL,
 description VARCHAR(1000) NOT NULL DEFAULT '',
 sort_order INTEGER NOT NULL DEFAULT 0,
 published BOOLEAN NOT NULL DEFAULT false,
 check_status VARCHAR(16) NOT NULL DEFAULT 'unchecked' CHECK(check_status IN ('unchecked','ok','failed')),
 last_checked_at TIMESTAMPTZ,
 last_error VARCHAR(500) NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 seed_key VARCHAR(80) UNIQUE,
 created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
 updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS public_feed_catalog_display ON public_feed_catalog(published,sort_order,id);
-- Managed using the admin DB. This table deliberately has no relationship to personal feeds.
REVOKE ALL ON public_feed_catalog FROM PUBLIC;

INSERT INTO public_feed_catalog(title,url,category,description,sort_order,seed_key) VALUES
('影视飓风','https://space.bilibili.com/946974','视频','影视科技测评',1,'popular-01'),
('罗翔说刑法','https://space.bilibili.com/517327498','视频','法律普法精品',2,'popular-02'),
('Kurzgesagt','https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q','视频','顶级科普动画',3,'popular-03'),
('Fireship','https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA','视频','高密度技术教学',4,'popular-04'),
('阮一峰的网络日志','https://www.ruanyifeng.com/blog/atom.xml','博客','科技爱好者周刊',5,'popular-05'),
('宝玉的分享','https://baoyu.io/feed.xml','博客','AI/工程译介',6,'popular-06'),
('Astral Codex Ten','https://astralcodexten.substack.com/feed','博客','理性主义通才博客',7,'popular-07'),
('The Honest Broker','https://www.honest-broker.com/feed','博客','文化与音乐评论',8,'popular-08'),
('商业就是这样','https://rsshub.app/xiaoyuzhou/podcast/6022a180ef5fdaddc30bb101','播客','第一财经商业播客',9,'popular-09'),
('不合时宜','https://rsshub.app/xiaoyuzhou/podcast/5e280fb8418a84a0461fd076','播客','国际政经文化对谈',10,'popular-10'),
('Lenny''s Newsletter','https://www.lennysnewsletter.com/feed','播客','产品经理访谈',11,'popular-11'),
('Acquired','https://www.acquired.fm/episodes?format=rss','播客','公司商业史长谈',12,'popular-12'),
('极客公园','https://www.geekpark.net/rss','科技','中文产品趋势',13,'popular-13'),
('Solidot','https://www.solidot.org/index.rss','科技','奇客新闻',14,'popular-14'),
('Stratechery','https://stratechery.com/feed/','科技','科技商业策略',15,'popular-15'),
('Platformer','https://www.platformer.news/feed','科技','平台与社交媒体',16,'popular-16'),
('量子位','https://www.qbitai.com/feed','AI','AI 业界动向',17,'popular-17'),
('36氪 AI','https://rsshub.app/36kr/news/AI','AI','36氪 AI 产业资讯',18,'popular-18'),
('Anthropic News','https://www.anthropic.com/news/feed.xml','AI','Anthropic 官方',19,'popular-19'),
('One Useful Thing','https://www.oneusefulthing.org/feed','AI','Ethan Mollick 的 AI 实用解读',20,'popular-20'),
('思想健康','https://rsshub.app/xiaoyuzhou/podcast/63d49e8c531dadd2b1b37fa3','健康','营养师健康科普',21,'popular-21'),
('果壳科学人','https://rsshub.app/guokr/scientific','健康','科学/健康频道',22,'popular-22'),
('Harvard Health Blog','https://www.health.harvard.edu/blog/feed','健康','哈佛医学院',23,'popular-23'),
('STAT News','https://www.statnews.com/feed/','健康','医学健康新闻',24,'popular-24'),
('少数派','https://sspai.com/feed','新闻','数字生活方式',25,'popular-25'),
('澎湃新闻','https://rsshub.app/thepaper/featured','新闻','时政深度',26,'popular-26'),
('The Free Press','https://www.thefp.com/feed','新闻','Bari Weiss 中立独立新闻',27,'popular-27'),
('Letters from an American','https://heathercoxrichardson.substack.com/feed','新闻','美国时政历史视角',28,'popular-28')
ON CONFLICT DO NOTHING;
