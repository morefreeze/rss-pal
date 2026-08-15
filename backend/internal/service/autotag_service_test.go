package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/bytedance/rss-pal/internal/autotag"
)

type fakeUserTagVocabulary struct {
	names []string
	err   error
}

func (f fakeUserTagVocabulary) GetTagNamesForUser(userID int) ([]string, error) {
	return f.names, f.err
}

type fakeArticleTagBinder struct {
	names []string
	err   error
}

func (f *fakeArticleTagBinder) BindByName(articleID, userID int, name string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.names = append(f.names, name)
	return len(f.names), nil
}

func TestAutoTagServiceBindsExistingTagAboveThreshold(t *testing.T) {
	binder := &fakeArticleTagBinder{}
	svc := NewAutoTagService(fakeUserTagVocabulary{names: []string{"OpenAI"}}, binder)

	decisions, err := svc.BindGeneratedTags(42, 7, []string{"Open AI", "文章"}, nil)
	if err != nil {
		t.Fatalf("BindGeneratedTags: %v", err)
	}

	if !reflect.DeepEqual(binder.names, []string{"OpenAI"}) {
		t.Fatalf("bound names = %#v, want OpenAI", binder.names)
	}
	if len(decisions) != 1 || decisions[0].Action != autotag.ReuseExisting || decisions[0].Similarity < 0.8 {
		t.Fatalf("decisions = %#v, want reused existing tag above threshold", decisions)
	}
}

func TestAutoTagServiceUsesGlobalVocabularyForSparseUsers(t *testing.T) {
	binder := &fakeArticleTagBinder{}
	svc := NewAutoTagService(fakeUserTagVocabulary{}, binder)

	decisions, err := svc.BindGeneratedTags(42, 7, []string{"向量 DB", "RAG"}, []string{"向量数据库"})
	if err != nil {
		t.Fatalf("BindGeneratedTags: %v", err)
	}

	if !reflect.DeepEqual(binder.names, []string{"向量数据库", "RAG"}) {
		t.Fatalf("bound names = %#v, want resolved global tag plus new cold-start tag", binder.names)
	}
	if decisions[0].Action != autotag.ReuseExisting {
		t.Fatalf("first decision = %#v, want reuse of global vocabulary tag", decisions[0])
	}
	if decisions[1].Action != autotag.CreateNew {
		t.Fatalf("second decision = %#v, want creation of RAG", decisions[1])
	}
}

func TestAutoTagServiceStopsOnBindError(t *testing.T) {
	wantErr := errors.New("bind failed")
	svc := NewAutoTagService(fakeUserTagVocabulary{names: []string{"OpenAI"}}, &fakeArticleTagBinder{err: wantErr})

	_, err := svc.BindGeneratedTags(42, 7, []string{"Open AI"}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
