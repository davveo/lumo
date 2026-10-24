(function () {
  'use strict';
  const C = window.LumoConstants;
  const viewState = {
    productId: '',
    reviewFilter: C.ReviewFilter.All,
    reviewSort: 'default',
    reviewPage: 1,
    questionFilter: 'all',
  };
  const findProduct = (id) => products.find((product) => product.id === id);
  const safeText = (value, length = 300) => (typeof value === 'string' ? value.trim().slice(0, length) : '');
  const validId = (value) => typeof value === 'string' && /^[a-zA-Z0-9_-]{1,150}$/.test(value);
  const currentName = () => safeText(window.LumoMember?.model()?.profile?.nickname, 30) || 'LUMO 生活家';
  const time = (value) =>
    new Date(value).toLocaleDateString('zh-CN', {
      timeZone: 'Asia/Shanghai',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    });
  const redraw = () => {
    if (typeof renderKeepScroll === 'function') renderKeepScroll();
  };
  const stars = (rating) => {
    const count = Math.max(0, Math.min(5, Math.round(Number(rating) || 0)));
    return `<span class="pex-stars" aria-label="${rating} 星评价">${'★'.repeat(count)}<i>${'★'.repeat(5 - count)}</i></span>`;
  };
  let cache = new Map(),
    ownerKey = '',
    loading = new Map(),
    lastProductView = '';
  const userId = () => {
    const u = window.LumoAuth?.user?.();
    return u?.id || u?.user_id || '';
  };
  const scope = () =>
    JSON.stringify([userId(), window.LumoAPI.getBase?.() || location.origin, window.LumoAPI.getToken?.() || '']);
  const api = (action, command, options) => window.LumoAPI.call(C.Service.Interaction, action, command, options);
  function storage() {
    const helpful = {},
      questions = [];
    for (const record of cache.values()) {
      record.reviews.forEach((row) => {
        if (row.liked) helpful[row.id] = true;
      });
      questions.push(...record.questions);
    }
    return { helpful, questions };
  }
  function rowView(row) {
    const mine = row.user_id === userId();
    return {
      ...row,
      productId: row.sku_id,
      author: mine ? currentName() : '用户 ' + String(row.user_id || '').slice(0, 1) + '***',
      rating: Number(row.rating) || 5,
      body: safeText(row.body, 4000),
      at: Date.parse(row.created_at) || 0,
      option: '',
      color: '',
      orderId: row.order_id,
      sample: !mine,
      baseHelpful: Number(row.helpful_count) || 0,
      publicationStatus: row.status,
      status:
        row.status === C.InteractionStatus.Rejected
          ? C.PublicationViewStatus.Rejected
          : row.status === C.InteractionStatus.Pending
            ? C.PublicationViewStatus.Moderating
            : row.answer
              ? C.PublicationViewStatus.Answered
              : C.PublicationViewStatus.Pending,
    };
  }
  async function refresh(productId = '') {
    if (ownerKey !== scope()) {
      cache.clear();
      ownerKey = scope();
      loading.clear();
    }
    if (!productId) {
      const route = (location.hash || '#/').slice(2).split('/');
      productId = route[0] === 'product' ? route[1] : '';
    }
    if (!productId || !findProduct(productId)) return;
    const owner = scope(),
      loadingKey = owner + '|' + productId,
      ticket = window.LumoInventory?.hydrationTicket?.(),
      previous = cache.get(productId);
    if (loading.has(loadingKey)) return loading.get(loadingKey);
    const run = (async () => {
      const options = { auth: !!window.LumoAuth?.isLoggedIn() };
      try {
        const requests = [C.InteractionKind.Review, C.InteractionKind.Question].map(async (kind) => {
          const command = { data: { kind, sku_id: productId, limit: 100 } };
          const [published, mine] = await Promise.all([
            api(C.Action.List, command, options),
            window.LumoAuth?.isLoggedIn() ? api(C.Action.List, { data: { ...command.data, mine: true } }) : null,
          ]);
          const rows = new Map((published.items || []).map((row) => [row.id, row]));
          (mine?.items || []).forEach((row) => rows.set(row.id, row));
          return [...rows.values()].map(rowView);
        });
        const [reviews, questions, related] = await Promise.all([
          ...requests,
          window.LumoAPI.call(
            C.Service.Recommendation,
            C.Action.List,
            { data: { scene: C.RecommendationScene.Similar, product_id: productId, limit: 8 } },
            options,
          ).catch((error) => ({ items: [], read_error: error.message || '相似商品加载失败' })),
        ]);
        if (owner !== scope()) return;
        const relatedRows = related.read_error
          ? previous?.related || []
          : window.LumoRecommendations?.mapItems
            ? window.LumoRecommendations.mapItems(related, C.RecommendationScene.Similar, [productId], ticket)
            : (related.items || [])
                .map((raw) => ({
                  product: findProduct(raw.product_id || raw.id),
                  reason: safeText(raw.recommendation_reason, 100) || '为你精选的好物',
                }))
                .filter((row) => row.product && row.product.id !== productId);
        cache.set(productId, {
          reviews,
          questions,
          related: relatedRows,
          relatedError: related.read_error || '',
          loaded: true,
          loadedAt: Date.now(),
          error: '',
        });
      } catch (error) {
        if (owner === scope())
          cache.set(productId, { reviews: [], questions: [], ...previous, loadedAt: Date.now(), error: error.message });
        throw error;
      } finally {
        loading.delete(loadingKey);
      }
    })();
    loading.set(loadingKey, run);
    return run;
  }
  function readState(productId) {
    const row = ownerKey === scope() ? cache.get(productId) : null;
    return {
      loaded: row?.loaded === true,
      loading: loading.has(scope() + '|' + productId) || !row,
      error: row?.error || '',
    };
  }
  function notice(productId) {
    const message = readState(productId).error;
    return message
      ? `<div class="pex-empty" role="alert"><p>${esc(message)}</p><button class="outline" data-product-extra-action="retry" data-product="${esc(productId)}">重新加载</button></div>`
      : '';
  }
  function activate(productId) {
    if (viewState.productId !== productId)
      Object.assign(viewState, {
        productId,
        reviewFilter: C.ReviewFilter.All,
        reviewSort: 'default',
        reviewPage: 1,
        questionFilter: 'all',
      });
  }
  function reviewData(productId) {
    return ownerKey === scope() ? cache.get(productId)?.reviews || [] : [];
  }
  function reviewCount(productId) {
    return reviewData(productId).length;
  }
  function reviews(productId) {
    const product = findProduct(productId);
    if (!product) return '<div class="pex-empty">该商品暂不可用。</div>';
    const read = readState(productId);
    if (!read.loaded)
      return `<section class="pex-loading">${read.error ? notice(productId) : window.LumoSkeleton?.render('reviews', { count: 3, label: '正在加载商品评价与问答' }) || ''}</section>`;
    activate(productId);
    const all = reviewData(productId),
      helpful = storage().helpful;
    const published = all.filter((row) => row.publicationStatus === C.InteractionStatus.Published);
    const average = published.length
      ? (published.reduce((sum, row) => sum + row.rating, 0) / published.length).toFixed(1)
      : '0.0';
    const favorable = published.length
      ? Math.round((published.filter((row) => row.rating >= 4).length / published.length) * 100)
      : 0;
    const filters = [
      ['all', '全部评价', all.length],
      ['good', '好评', all.filter((row) => row.rating >= 4).length],
      ['low', '中差评', all.filter((row) => row.rating <= 3).length],
      ['mine', '我的评价', all.filter((row) => !row.sample).length],
    ];
    let list = all.filter(
      (row) =>
        viewState.reviewFilter === C.ReviewFilter.All ||
        (viewState.reviewFilter === C.ReviewFilter.Good && row.rating >= 4) ||
        (viewState.reviewFilter === C.ReviewFilter.Low && row.rating <= 3) ||
        (viewState.reviewFilter === C.ReviewFilter.Mine && !row.sample),
    );
    list.sort((a, b) =>
      viewState.reviewSort === 'recent'
        ? b.at - a.at
        : Number(a.sample) - Number(b.sample) || b.baseHelpful - a.baseHelpful || b.at - a.at,
    );
    const pages = Math.max(1, Math.ceil(list.length / 6));
    viewState.reviewPage = Math.min(viewState.reviewPage, pages);
    const visible = list.slice((viewState.reviewPage - 1) * 6, viewState.reviewPage * 6);
    return `<section class="pex-reviews" aria-label="商品评价">${notice(productId)}<div class="pex-review-overview"><div class="pex-rating-score"><strong>${average}<small>/ 5</small></strong>${stars(Number(average))}<p>${favorable}% 好评 · ${published.length} 条公开评价</p></div><div class="pex-rating-bars">${[
      5, 4, 3, 2, 1,
    ]
      .map((score) => {
        const count = published.filter((row) => row.rating === score).length;
        return `<div><span>${score} 星</span><i><b style="width:${published.length ? Math.round((count / published.length) * 100) : 0}%"></b></i><small>${count}</small></div>`;
      })
      .join(
        '',
      )}</div><div class="pex-review-invite">${icon('edit')}<div><strong>分享你的购物体验</strong><p>收货后可在订单中评价商品。</p><a href="#/member/orders/review">去评价 ${icon('chevron')}</a></div></div></div><div class="pex-review-toolbar"><div class="pex-chips" aria-label="评价筛选">${filters.map(([value, label, count]) => `<button data-product-extra-action="review-filter" data-value="${value}" data-product="${esc(productId)}" class="${viewState.reviewFilter === value ? 'active' : ''}" aria-pressed="${viewState.reviewFilter === value}">${label}<span>${count}</span></button>`).join('')}</div><label class="pex-sort-label">排序<select id="pex-review-sort" aria-label="评价排序"><option value="default" ${viewState.reviewSort === 'default' ? 'selected' : ''}>推荐排序</option><option value="recent" ${viewState.reviewSort === 'recent' ? 'selected' : ''}>最新评价</option></select></label></div><div class="pex-review-list">${visible.length ? visible.map((row) => `<article class="pex-review-card"><div class="pex-review-author"><span class="pex-avatar">${esc(row.author.slice(0, 1))}</span><div><strong>${esc(row.author)}</strong><small>${row.sample ? '购买评价' : row.status === C.PublicationViewStatus.Moderating ? '我的评价 · 待审核' : row.status === C.PublicationViewStatus.Rejected ? '我的评价 · 未通过审核' : '我的购买评价'}</small></div><time>${time(row.at)}</time></div><div class="pex-review-content">${stars(row.rating)}<p>${esc(row.body)}</p><div class="pex-review-meta"><span>${esc(row.color)} / ${esc(row.option)}</span>${row.orderId ? `<button data-action="order-details" data-id="${esc(row.orderId)}">查看订单</button>` : ''}<button data-product-extra-action="helpful" data-product="${esc(productId)}" data-id="${esc(row.id)}" aria-pressed="${!!helpful[row.id]}" aria-label="${helpful[row.id] ? '已标记有帮助' : '这条评价有帮助'}：${esc(row.author)}" class="pex-helpful ${helpful[row.id] ? 'active' : ''}" ${helpful[row.id] || row.status === C.PublicationViewStatus.Moderating || row.status === C.PublicationViewStatus.Rejected ? 'disabled' : ''}>${icon('check')}有帮助 ${row.baseHelpful}</button></div></div></article>`).join('') : '<div class="pex-empty">暂无这类评价，看看其他评价吧。</div>'}</div>${pages > 1 ? `<div class="pex-pagination"><button data-product-extra-action="review-page" data-value="${viewState.reviewPage - 1}" ${viewState.reviewPage <= 1 ? 'disabled' : ''}>上一页</button><span>${viewState.reviewPage} / ${pages}</span><button data-product-extra-action="review-page" data-value="${viewState.reviewPage + 1}" ${viewState.reviewPage >= pages ? 'disabled' : ''}>下一页</button></div>` : ''}</section>`;
  }
  function questionData(productId) {
    return ownerKey === scope() ? cache.get(productId)?.questions || [] : [];
  }
  function questionCount(productId) {
    return questionData(productId).length;
  }
  function questions(productId) {
    const product = findProduct(productId);
    if (!product) return '<div class="pex-empty">该商品暂不可用。</div>';
    const read = readState(productId);
    if (!read.loaded)
      return `<section class="pex-loading">${read.error ? notice(productId) : window.LumoSkeleton?.render('reviews', { count: 3, label: '正在加载商品评价与问答' }) || ''}</section>`;
    activate(productId);
    const all = questionData(productId),
      list = all.filter((row) => viewState.questionFilter === 'all' || row.status === viewState.questionFilter);
    return `<section class="pex-questions" aria-label="商品问答">${notice(productId)}<div class="pex-question-head"><div><h3>买前问一问，好物更合心</h3><p>${all.length} 个问题 · 已解答 ${all.filter((row) => row.status === C.PublicationViewStatus.Answered).length} 个</p></div><button class="pex-primary" data-product-extra-action="ask" data-product="${esc(productId)}" ${product.visible === false ? 'disabled' : ''}>${icon('plus')}我要提问</button></div><div class="pex-chips pex-question-filters">${[
      ['all', '全部问题'],
      [C.PublicationViewStatus.Answered, '已解答'],
      [C.PublicationViewStatus.Pending, '待回复'],
      [C.PublicationViewStatus.Moderating, '审核中'],
    ]
      .map(
        ([value, label]) =>
          `<button data-product-extra-action="question-filter" data-value="${value}" data-product="${esc(productId)}" class="${viewState.questionFilter === value ? 'active' : ''}" aria-pressed="${viewState.questionFilter === value}">${label}</button>`,
      )
      .join(
        '',
      )}</div><div class="pex-question-list">${list.length ? list.map((row) => `<article class="pex-question-card"><div class="pex-question-title"><span class="pex-question-symbol">问</span><h4>${esc(row.body)}</h4><span class="pex-question-status ${row.status === C.PublicationViewStatus.Pending ? C.PublicationViewStatus.Pending : ''}">${{ pending: '待回复', answered: '已解答', moderating: '审核中', rejected: '未通过审核' }[row.status]}</span></div>${row.answer ? `<div class="pex-answer"><span class="pex-question-symbol">答</span><p>${esc(row.answer)}</p></div>` : '<p class="pex-question-pending">问题已保存，可在这里查看回复状态。</p>'}<div class="pex-question-meta"><span>${esc(row.author)}</span><time>${time(row.at)}</time>${!row.sample ? '<span>我的提问</span>' : ''}</div></article>`).join('') : '<div class="pex-empty">这里还没有问题。</div>'}</div></section>`;
  }
  function ask(productId) {
    const product = findProduct(productId);
    if (!product || product.visible === false) {
      toast('该商品暂不可提问');
      return;
    }
    if (window.LumoAuth && !window.LumoAuth.requireLogin('#/product/' + productId)) return;
    modal(
      '关于这件好物，你想了解什么？',
      `<p class="modal-desc">${esc(product.name)}</p><form id="product-question-form" data-product="${esc(productId)}"><label class="field">你的问题<textarea name="question" rows="4" minlength="5" maxlength="180" required placeholder="描述你关心的规格、使用或服务问题（5–180 字）"></textarea></label><div class="pex-question-form-foot"><span>提问人：${esc(currentName())}</span><button class="primary" type="submit">提交问题</button></div></form>`,
    );
  }
  function detailActions(product) {
    if (!product || !findProduct(product.id)) return '';
    const saved = state.favorites.includes(product.id);
    return `<div class="pex-detail-actions"><button data-action="favorite" data-id="${esc(product.id)}" aria-pressed="${saved}" class="${saved ? 'active' : ''}">${icon('heart')}${saved ? '已收藏' : '收藏好物'}</button><button data-product-extra-action="share" data-product="${esc(product.id)}">${icon('arrow')}分享商品</button><button data-product-extra-action="show-questions" data-product="${esc(product.id)}">${icon('info')}商品问答</button></div>`;
  }
  function related(product) {
    if (!product || !findProduct(product.id)) return '';
    const rows =
      ownerKey === scope()
        ? (cache.get(product.id)?.related || [])
            .filter((row) => row.product.id !== product.id && row.product.visible !== false)
            .slice(0, 4)
        : [];
    const read = readState(product.id);
    if (!read.loaded && !read.error)
      return `<section class="pex-related"><div class="section-head"><h2>相似好物</h2></div>${window.LumoSkeleton?.render('products', { count: 3, label: '正在加载相似商品' }) || ''}</section>`;
    const failure = read.error || (ownerKey === scope() ? cache.get(product.id)?.relatedError : '');
    if (failure && !rows.length)
      return `<section class="pex-related"><h2>相似好物</h2><div class="pex-empty" role="alert"><p>${esc(failure)}</p><button class="outline" data-product-extra-action="retry" data-product="${esc(product.id)}">重新加载</button></div></section>`;
    window.LumoRecommendations?.restoreItems?.(C.RecommendationScene.Similar, rows);
    return rows.length
      ? `<section class="pex-related"><div class="section-head"><div><h2>相似好物</h2><p>结合当前商品特征，为你精选。</p></div><a class="text-button" href="#/">浏览全部${icon('chevron')}</a></div>${failure ? `<p role="alert">${esc(failure)}</p>` : ''}<div class="product-grid">${rows.map((row) => `<div class="home-recommendation-card">${productCard(row.product)}<div class="purchase-recommendation-reason">${icon('sparkles')}<span>${esc(row.reason)}</span></div></div>`).join('')}</div></section>`
      : '';
  }
  async function share(productId) {
    if (!findProduct(productId)) return;
    let url = '#/product/' + productId;
    try {
      url = location.origin + location.pathname + '#/product/' + encodeURIComponent(productId);
    } catch (_) {}
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(url);
      toast('商品链接已复制');
    } catch (_) {
      modal(
        '分享这件好物',
        `<p class="modal-desc">复制下方商品链接，即可再次打开。</p><label class="field">商品链接<input type="text" readonly value="${esc(url)}" aria-label="商品链接"></label><button class="primary" data-action="close-modal">完成</button>`,
      );
    }
  }
  function afterRender() {
    if (window.LumoConnection?.ready === false || window.LumoAPI?.isBootstrapActive?.()) return;
    const route = (location.hash || '#/').slice(2).split('/');
    if (route[0] !== 'product' || !findProduct(route[1])) {
      lastProductView = '';
      return;
    }
    const viewKey = scope() + '|' + route[1];
    if (window.LumoAuth?.isLoggedIn() && viewKey !== lastProductView) {
      lastProductView = viewKey;
      const key = 'product-view-' + (crypto.randomUUID?.() || Date.now().toString(36));
      window.LumoAPI.call(C.Service.Analytics, C.Action.Record, {
        idempotency_key: key,
        data: { type: C.AnalyticsBehavior.View, sku_id: route[1] },
      }).catch(() => {});
    }
    const current = cache.get(route[1]);
    if (ownerKey !== scope() || !current || Date.now() - current.loadedAt > 30000)
      refresh(route[1])
        .then(redraw)
        .catch((error) => {
          toast(error.message);
          redraw();
        });
  }
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-product-extra-action]');
    if (!button || button.disabled) return;
    const action = button.dataset.productExtraAction,
      productId = button.dataset.product,
      id = button.dataset.id;
    if (action === 'retry') {
      button.disabled = true;
      refresh(productId)
        .then(redraw)
        .catch((error) => {
          toast(error.message);
          redraw();
        });
      return;
    }
    if (action === 'share') {
      share(productId);
      return;
    }
    if (action === 'ask') {
      ask(productId);
      return;
    }
    if (action === 'show-questions') {
      if (!findProduct(productId)) return;
      ui.detailTab = 3;
      redraw();
      document.querySelector('.details-bottom')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
      return;
    }
    if (action === 'review-filter') {
      if (
        ![C.ReviewFilter.All, C.ReviewFilter.Good, C.ReviewFilter.Low, C.ReviewFilter.Mine].includes(
          button.dataset.value,
        )
      )
        return;
      activate(productId);
      viewState.reviewFilter = button.dataset.value;
      viewState.reviewPage = 1;
      redraw();
      return;
    }
    if (action === 'review-page') {
      const next = Number(button.dataset.value);
      if (!Number.isInteger(next) || next < 1) return;
      viewState.reviewPage = next;
      redraw();
      return;
    }
    if (action === 'question-filter') {
      if (
        ![
          'all',
          C.PublicationViewStatus.Answered,
          C.PublicationViewStatus.Pending,
          C.PublicationViewStatus.Moderating,
        ].includes(button.dataset.value)
      )
        return;
      activate(productId);
      viewState.questionFilter = button.dataset.value;
      redraw();
      return;
    }
    if (action === 'helpful') {
      if (!window.LumoAuth?.requireLogin('#/product/' + productId)) return;
      const row = reviewData(productId).find((row) => row.id === id);
      if (!row || row.liked) return;
      button.disabled = true;
      try {
        const result = await api(C.InteractionKind.Like, { id });
        row.liked = result.liked;
        row.baseHelpful = Number(result.helpful_count) || 0;
        redraw();
      } catch (error) {
        toast(error.message);
        button.disabled = false;
      }
    }
  });
  document.addEventListener('change', (event) => {
    if (event.target.id === 'pex-review-sort') {
      viewState.reviewSort = event.target.value === 'recent' ? 'recent' : 'default';
      viewState.reviewPage = 1;
      redraw();
    }
  });
  document.addEventListener('submit', async (event) => {
    const form = event.target;
    if (form.id !== 'product-question-form') return;
    event.preventDefault();
    const productId = form.dataset.product,
      product = findProduct(productId);
    if (window.LumoAuth && !window.LumoAuth.requireLogin('#/product/' + productId)) return;
    if (!product || product.visible === false) {
      toast('该商品暂不可提问');
      return;
    }
    const body = safeText(new FormData(form).get('question'), 180);
    if (body.length < 5) {
      toast('请至少填写 5 个字，描述你的问题');
      return;
    }
    const button = form.querySelector('button[type="submit"]');
    if (button) button.disabled = true;
    form.dataset.requestKey ||= 'question-' + (crypto.randomUUID?.() || Date.now().toString(36));
    try {
      await api(C.Action.Create, {
        idempotency_key: form.dataset.requestKey,
        data: { kind: C.InteractionKind.Question, sku_id: productId, body },
      });
      await refresh(productId);
      closeModal();
      ui.detailTab = 3;
      viewState.questionFilter = C.PublicationViewStatus.Moderating;
      redraw();
      toast('问题已提交，等待审核回复');
    } catch (error) {
      toast(error.message);
      if (button) button.disabled = false;
    }
  });
  window.addEventListener?.('lumo:ready', afterRender);
  window.LumoProductExtras = {
    readState,
    reviews,
    reviewCount,
    reviewData,
    questions,
    questionCount,
    questionData,
    detailActions,
    related,
    panel: (product, tab) => (tab === 3 || tab === 'questions' ? questions(product.id) : reviews(product.id)),
    afterRender,
    refresh,
    share,
  };
})();
