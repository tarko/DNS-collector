package transformers

import (
	"fmt"

	"github.com/dmachard/go-dnscollector/dnsutils"
	"github.com/dmachard/go-dnscollector/pkgconfig"
	"github.com/dmachard/go-logger"
)

var (
	ReturnError = 0
	ReturnKeep  = 1
	ReturnDrop  = 2
)

type Subtransform struct {
	name        string
	processFunc func(dm *dnsutils.DNSMessage) (int, error)
}

type Transformation interface {
	GetTransforms() ([]Subtransform, error)
	ReloadConfig(config *pkgconfig.ConfigTransformers)
	Reset()
}

type GenericTransformer struct {
	config            *pkgconfig.ConfigTransformers
	logger            *logger.Logger
	name              string
	nextWorkers       []chan dnsutils.DNSMessage
	LogInfo, LogError func(msg string, v ...interface{})
}

func NewTransformer(config *pkgconfig.ConfigTransformers, logger *logger.Logger, name string, workerName string, instance int, nextWorkers []chan dnsutils.DNSMessage) GenericTransformer {
	t := GenericTransformer{config: config, logger: logger, nextWorkers: nextWorkers, name: name}

	t.LogInfo = func(msg string, v ...interface{}) {
		log := fmt.Sprintf("worker - [%s] (conn #%d) [transform=%s] - ", workerName, instance, name)
		logger.Info(log+msg, v...)
	}

	t.LogError = func(msg string, v ...interface{}) {
		log := fmt.Sprintf("worker - [%s] (conn #%d) [transform=%s] - ", workerName, instance, name)
		logger.Error(log+msg, v...)
	}
	return t
}

func (t *GenericTransformer) ReloadConfig(config *pkgconfig.ConfigTransformers) {
	t.config = config
}

func (t *GenericTransformer) Reset() {}

type TransformEntry struct {
	Transformation
}

type Transforms struct {
	config   *pkgconfig.ConfigTransformers
	logger   *logger.Logger
	name     string
	instance int

	availableTransforms     []TransformEntry
	activeTransforms        []TransformEntry
	activeProcessTransforms []func(dm *dnsutils.DNSMessage) (int, error)
}

func NewTransforms(config *pkgconfig.ConfigTransformers, logger *logger.Logger, name string, nextWorkers []chan dnsutils.DNSMessage, instance int) Transforms {

	d := Transforms{config: config, logger: logger, name: name, instance: instance}

	// order definition important
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewNormalizeTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewFilteringTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewReducerTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewATagsTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewRestTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewRelabelTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewUserPrivacyTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewExtractTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewSuspiciousTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewMachineLearningTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewLatencyTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewDNSGeoIPTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewRewriteTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewNewDomainTrackerTransform(config, logger, name, instance, nextWorkers)})
	d.availableTransforms = append(d.availableTransforms, TransformEntry{NewReorderingTransform(config, logger, name, instance, nextWorkers)})

	d.Prepare()
	return d
}

func (p *Transforms) ReloadConfig(config *pkgconfig.ConfigTransformers) {
	p.config = config

	for _, transform := range p.availableTransforms {
		transform.ReloadConfig(config)
	}

	p.Prepare()
}

func (p *Transforms) Prepare() error {
	// clean the slice
	p.activeProcessTransforms = p.activeProcessTransforms[:0]
	p.activeTransforms = p.activeTransforms[:0]
	tranformsList := []string{}

	for _, transform := range p.availableTransforms {
		subtransforms, err := transform.GetTransforms()
		if err != nil {
			p.LogError("error on init subtransforms: %v", err)
			continue
		}
		if len(subtransforms) > 0 {
			p.activeTransforms = append(p.activeTransforms, transform)
		}
		for _, subtransform := range subtransforms {
			p.activeProcessTransforms = append(p.activeProcessTransforms, subtransform.processFunc)

			tranformsList = append(tranformsList, subtransform.name)
		}
	}

	if len(tranformsList) > 0 {
		p.LogInfo("transformers applied: %v", tranformsList)
	}
	return nil
}

func (p *Transforms) Reset() {
	for _, transform := range p.activeTransforms {
		transform.Reset()
	}
}

func (p *Transforms) LogInfo(msg string, v ...interface{}) {
	connlog := fmt.Sprintf("(conn #%d) ", p.instance)
	p.logger.Info(pkgconfig.PrefixLogWorker+"["+p.name+"] "+connlog+msg, v...)
}

func (p *Transforms) LogError(msg string, v ...interface{}) {
	p.logger.Error(pkgconfig.PrefixLogWorker+"["+p.name+"] "+msg, v...)
}

func (p *Transforms) ProcessMessage(dm *dnsutils.DNSMessage) (int, error) {
	for _, transform := range p.activeProcessTransforms {
		if result, err := transform(dm); err != nil {
			return ReturnKeep, fmt.Errorf("error on transform processing: %v", err.Error())
		} else if result == ReturnDrop {
			return ReturnDrop, nil
		}
	}
	return ReturnKeep, nil
}
