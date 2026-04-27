--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: driver_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    company_id uuid,
    phone character varying(20) NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    invited_by_dispatcher_id uuid,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    responded_at timestamp with time zone,
    CONSTRAINT chk_driver_invitations_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'accepted'::character varying, 'declined'::character varying, 'cancelled'::character varying])::text[])))
);


--
-- Name: driver_invitations driver_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_pkey PRIMARY KEY (id);


--
-- Name: driver_invitations driver_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_token_key UNIQUE (token);


--
-- Name: idx_driver_invitations_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_company ON public.driver_invitations USING btree (company_id);


--
-- Name: idx_driver_invitations_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_dispatcher ON public.driver_invitations USING btree (invited_by_dispatcher_id) WHERE (invited_by_dispatcher_id IS NOT NULL);


--
-- Name: idx_driver_invitations_invited_by_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_invited_by_status_created ON public.driver_invitations USING btree (invited_by, status, created_at DESC);


--
-- Name: idx_driver_invitations_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_phone ON public.driver_invitations USING btree (phone);


--
-- Name: idx_driver_invitations_phone_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_invitations_phone_status_created ON public.driver_invitations USING btree (replace(replace(replace(TRIM(BOTH FROM phone), ' '::text, ''::text), '-'::text, ''::text), '+'::text, ''::text), status, created_at DESC);


--
-- Name: idx_driver_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_driver_invitations_token ON public.driver_invitations USING btree (token);


--
-- Name: driver_invitations driver_invitations_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT driver_invitations_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- Name: driver_invitations fk_driver_invitations_dispatcher; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_invitations
    ADD CONSTRAINT fk_driver_invitations_dispatcher FOREIGN KEY (invited_by_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--


